// Package xray manages the second Fibr protocol: Xray (VLESS + Reality) for
// DPI resistance.
//
// Fibr itself never speaks VLESS. It only:
//   - owns the Reality x25519 keypair (stored in the settings table, exactly
//     like the WireGuard server key);
//   - issues a client UUID per "xray" device (see store.Device for the
//     public_key = uuid convention);
//   - renders /data/xray/config.json on the shared docker volume, which the
//     separate xray container (compose profile "xray") reads.
//
// Applying changes: fibr has no access to the docker socket, so it cannot
// restart the sibling xray container. Instead the rendered config enables
// xray's gRPC API (HandlerService, port 10085, reachable only inside the
// compose network) and SyncClients adds/removes users LIVE through it
// (api.go). The config file is still rewritten atomically on every change,
// so a restart of the xray container always lands on the same state.
//
// Sharing limit (limiter.go): xray writes an access log naming the link
// (email = uuid) and the source IP of every connection; fibr counts
// distinct IPs per link and cuts a link that is used from more places at
// once than FIBR_XRAY_IP_LIMIT allows.
package xray

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/node"
	"github.com/fenjo26/Fibr/internal/store"
)

const (
	settingPrivKey = "xray_reality_private_key" // base64url(x25519 private key)
	settingPubKey  = "xray_reality_public_key"  // base64url(x25519 public key), cached copy
	settingShortID = "xray_reality_short_id"    // generated when FIBR_XRAY_SHORT_ID is empty

	// ClientFlow is the only client flow this MVP provisions.
	ClientFlow = "xtls-rprx-vision"
)

// Manager owns everything Xray-related: the Reality keypair and the rendered
// server config.
type Manager struct {
	cfg       *config.Config
	store     *store.Store
	log       *slog.Logger
	privB64   string // Reality private key (goes into config.json)
	pubKeyB64 string // Reality public key (goes into vless:// links as pbk=)

	api *APIClient // nil = live apply disabled (FIBR_XRAY_API=off)

	mu      sync.Mutex
	known   map[string]bool      // uuids ever pushed to xray (removal candidates)
	banned  map[string]time.Time // uuid -> ban end (sharing limiter)
	onLimit func(uuid string, ips []string)
}

// New creates the manager, loading or generating the Reality x25519 keypair.
// The private key lives in the settings table; the public key is stored next
// to it so restarts never need to re-derive it (and for cheap inspection).
func New(cfg *config.Config, st *store.Store, log *slog.Logger) (*Manager, error) {
	m := &Manager{
		cfg: cfg, store: st, log: log,
		api:    NewAPIClient(cfg.XrayAPIAddr),
		known:  map[string]bool{},
		banned: map[string]time.Time{},
	}

	privStr, err := st.GetSetting(settingPrivKey)
	switch {
	case err == nil:
		if _, perr := decodeX25519Private(privStr); perr != nil {
			return nil, fmt.Errorf("stored xray reality key is invalid: %w", perr)
		}
		m.privB64 = privStr
	case errors.Is(err, store.ErrNotFound):
		priv, gerr := ecdh.X25519().GenerateKey(rand.Reader)
		if gerr != nil {
			return nil, fmt.Errorf("generate reality key: %w", gerr)
		}
		privEnc := base64.RawURLEncoding.EncodeToString(priv.Bytes())
		pubEnc := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
		if serr := st.SetSetting(settingPrivKey, privEnc); serr != nil {
			return nil, fmt.Errorf("persist reality key: %w", serr)
		}
		if serr := st.SetSetting(settingPubKey, pubEnc); serr != nil {
			return nil, fmt.Errorf("persist reality public key: %w", serr)
		}
		m.privB64, m.pubKeyB64 = privEnc, pubEnc
		m.log.Info("generated new xray reality keypair")
	default:
		return nil, err
	}

	// Reality short id: FIBR_XRAY_SHORT_ID, or generated once and kept in
	// the settings table so links stay valid across restarts. Written back
	// into the shared config so every link renderer sees it.
	if cfg.XrayShortID == "" {
		sid, err := st.GetSetting(settingShortID)
		if errors.Is(err, store.ErrNotFound) {
			b := make([]byte, 4)
			if _, rerr := rand.Read(b); rerr != nil {
				return nil, fmt.Errorf("generate reality short id: %w", rerr)
			}
			sid = hex.EncodeToString(b)
			if serr := st.SetSetting(settingShortID, sid); serr != nil {
				return nil, fmt.Errorf("persist reality short id: %w", serr)
			}
		} else if err != nil {
			return nil, err
		}
		cfg.XrayShortID = sid
	}

	// Cached public key: fall back to deriving it from settings, then to a
	// decode of the private key (defensive; both are always written together).
	if m.pubKeyB64 == "" {
		if pubStr, err := st.GetSetting(settingPubKey); err == nil {
			m.pubKeyB64 = pubStr
		} else {
			priv, perr := decodeX25519Private(m.privB64)
			if perr != nil {
				return nil, fmt.Errorf("stored xray reality key is invalid: %w", perr)
			}
			m.pubKeyB64 = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
		}
	}
	return m, nil
}

// PublicKey returns the Reality public key in the base64url form used by
// clients (the pbk= parameter of vless:// links).
func (m *Manager) PublicKey() string { return m.pubKeyB64 }

// ShortID returns the shared Reality short id (sid= in client links).
func (m *Manager) ShortID() string { return m.cfg.XrayShortID }

// SNI returns the Reality camouflage domain.
func (m *Manager) SNI() string { return m.cfg.XraySNI }

// Port returns the xray TCP listen port.
func (m *Manager) Port() int { return m.cfg.XrayPort }

// GenerateRealityKeypair returns a fresh x25519 Reality keypair in the
// base64url wire format used by configs (private) and vless links (public).
// Used by the control-plane manager (settings-backed) and by node agents
// (file-backed) alike.
func GenerateRealityKeypair() (privB64, pubB64 string, err error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	privB64 = base64.RawURLEncoding.EncodeToString(priv.Bytes())
	pubB64 = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	return privB64, pubB64, nil
}

func decodeX25519Private(enc string) (*ecdh.PrivateKey, error) {
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPrivateKey(raw)
}

/* ---------------------------- client identity ---------------------------- */

// NewUUID returns a random RFC 4122 version 4 UUID — the client identity for
// a VLESS device. (Hand-rolled to avoid a new module dependency; the format
// is asserted in tests.)
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ClientLink renders the vless:// share URL for an xray device against this
// box's own endpoint. The UUID is taken from Device.PublicKey (see
// store.Device for the field convention); host comes from FIBR_WG_HOST and
// the port from FIBR_XRAY_PORT — xray has no separate public hostname in
// this MVP.
func (m *Manager) ClientLink(d store.Device) string {
	return m.ClientLinkAt(d, node.Endpoint{
		Host:          m.cfg.WGHost,
		XrayPort:      m.cfg.XrayPort,
		XrayPubKeyB64: m.pubKeyB64,
		XrayShortID:   m.cfg.XrayShortID,
		XraySNI:       m.cfg.XraySNI,
	})
}

// ClientLinkAt renders the link for an arbitrary node endpoint — with
// multi-node, the active node's host/port/keys replace this box's own (see
// internal/node).
func (m *Manager) ClientLinkAt(d store.Device, ep node.Endpoint) string {
	return fmt.Sprintf("vless://%s@%s:%d?type=tcp&security=reality&fp=chrome&sni=%s&pbk=%s&sid=%s&flow=%s&encryption=none#%s",
		d.PublicKey, ep.Host, ep.XrayPort, ep.XraySNI, ep.XrayPubKeyB64, ep.XrayShortID,
		ClientFlow, url.PathEscape("Fibr-"+d.Name))
}

/* ---------------------------- server config ---------------------------- */

// clientEntry is one VLESS client in the inbound clients list. Email is the
// uuid again: xray prints it in the access log ("email: <uuid>"), which is
// how the sharing limiter knows which link a connection used, and it is the
// key RemoveUser works with.
type clientEntry struct {
	ID    string `json:"id"`
	Flow  string `json:"flow"`
	Email string `json:"email"`
}

type inboundSettings struct {
	Clients    []clientEntry `json:"clients"`
	Decryption string        `json:"decryption"` // VLESS: always "none"
}

type realitySettings struct {
	Show        bool     `json:"show"`
	Dest        string   `json:"dest"`
	Xver        int      `json:"xver"`
	ServerNames []string `json:"serverNames"`
	PrivateKey  string   `json:"privateKey"`
	ShortIds    []string `json:"shortIds"`
}

type streamSettings struct {
	Network         string          `json:"network"`
	Security        string          `json:"security"`
	RealitySettings realitySettings `json:"realitySettings"`
}

type sniffingSettings struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
}

type inbound struct {
	Tag            string           `json:"tag"`
	Listen         string           `json:"listen"`
	Port           int              `json:"port"`
	Protocol       string           `json:"protocol"`
	Settings       inboundSettings  `json:"settings"`
	StreamSettings streamSettings   `json:"streamSettings"`
	Sniffing       sniffingSettings `json:"sniffing"`
}

type outbound struct {
	Protocol string `json:"protocol"`
	Tag      string `json:"tag"`
}

type apiSettings struct {
	Tag      string   `json:"tag"`
	Listen   string   `json:"listen"`
	Services []string `json:"services"`
}

type serverConfig struct {
	Log struct {
		LogLevel string `json:"loglevel"`
		Access   string `json:"access,omitempty"`
	} `json:"log"`
	API       *apiSettings `json:"api,omitempty"`
	Inbounds  []inbound    `json:"inbounds"`
	Outbounds []outbound   `json:"outbounds"`
}

// BuildServerConfig renders the xray-core config for the given client UUIDs:
// one VLESS inbound with Reality over TCP, flow xtls-rprx-vision, using this
// box's own key/port/camouflage settings.
//
// Unlike node configs it also turns on the access log and the gRPC API that
// the control plane's live apply and sharing limiter use.
func (m *Manager) BuildServerConfig(uuids []string) ([]byte, error) {
	return buildConfig(m.cfg.XrayPort, m.cfg.XraySNI, m.cfg.XrayDest, m.privB64, m.cfg.XrayShortID, uuids, true)
}

// BuildStandaloneConfig renders an xray-core config for an arbitrary Reality
// setup — the node agents (cmd/fibr-node) own their keypair and short id in
// local files instead of the control plane's settings table, so they render
// through this function with their own values.
func BuildStandaloneConfig(listenPort int, sni, dest, privKeyB64, shortID string, uuids []string) ([]byte, error) {
	return buildConfig(listenPort, sni, dest, privKeyB64, shortID, uuids, false)
}

// buildConfig renders the config; control adds the access log + gRPC API.
func buildConfig(listenPort int, sni, dest, privKeyB64, shortID string, uuids []string, control bool) ([]byte, error) {
	clients := make([]clientEntry, 0, len(uuids))
	for _, id := range uuids {
		clients = append(clients, clientEntry{ID: id, Flow: ClientFlow, Email: id})
	}
	cfg := serverConfig{}
	cfg.Log.LogLevel = "warning"
	// Access log on the shared volume (the sharing limiter reads it) and the
	// gRPC API for live user changes. The API listens on all interfaces of
	// the xray CONTAINER, i.e. only the compose network: the port is not
	// published to the host.
	if control {
		cfg.Log.Access = AccessLogPath
		cfg.API = &apiSettings{Tag: "api", Listen: "0.0.0.0:10085", Services: []string{"HandlerService"}}
	}
	cfg.Inbounds = []inbound{{
		Tag:      InboundTag,
		Listen:   "0.0.0.0",
		Port:     listenPort,
		Protocol: "vless",
		Settings: inboundSettings{Clients: clients, Decryption: "none"},
		StreamSettings: streamSettings{
			Network:  "tcp",
			Security: "reality",
			RealitySettings: realitySettings{
				Show:        false,
				Dest:        dest,
				Xver:        0,
				ServerNames: []string{sni},
				PrivateKey:  privKeyB64,
				ShortIds:    []string{shortID},
			},
		},
		Sniffing: sniffingSettings{Enabled: true, DestOverride: []string{"http", "tls", "quic"}},
	}}
	cfg.Outbounds = []outbound{
		{Protocol: "freedom", Tag: "direct"},
		{Protocol: "blackhole", Tag: "block"},
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// ConfigPath is the on-disk location of the rendered xray config, inside the
// docker volume shared with the xray container.
func (m *Manager) ConfigPath() string { return filepath.Join(m.cfg.DataDir, "xray", "config.json") }

// SyncClients rebuilds /data/xray/config.json from the active xray devices.
// No-op (nil) when xray is disabled in config. The file is replaced
// atomically; the xray container must be restarted to pick it up:
//
//	docker compose restart xray
//
// The device source is ListActiveDevices (the store's expiry-aware view):
// an xray device whose owner is past their subscription deadline must be
// dropped from the inbound exactly like a wg peer would be (see
// docs/billing.md).
func (m *Manager) SyncClients() error {
	if !m.cfg.XrayEnabled {
		return nil
	}
	devices, err := m.store.ListActiveDevices(time.Now())
	if err != nil {
		return err
	}
	uuids := make([]string, 0, len(devices))
	for _, d := range devices {
		if d.Protocol == store.ProtocolXray {
			uuids = append(uuids, d.PublicKey) // uuid, see store.Device
		}
	}
	data, err := m.BuildServerConfig(uuids)
	if err != nil {
		return err
	}
	path := m.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	// 0644: the sibling xray container reads the file through the shared
	// volume, possibly as a different user.
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	m.log.Info("xray config written", "path", path, "clients", len(uuids))
	m.applyLive(uuids)
	return nil
}

// AccessLogPath is where xray writes its access log (the /data volume is
// shared by the fibr and xray containers).
const AccessLogPath = "/data/xray/access.log"

// applyLive pushes the client set to the running xray through its API:
// adds every desired uuid that is not banned, removes the ones that are
// gone (or banned). Best-effort: when the API is unreachable (old xray
// container, FIBR_XRAY_API=off) a restart of the container still applies
// the freshly written file.
func (m *Manager) applyLive(uuids []string) {
	if m.api == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Now()
	m.mu.Lock()
	desired := make(map[string]bool, len(uuids))
	for _, u := range uuids {
		if end, ok := m.banned[u]; ok && now.Before(end) {
			continue // cut by the sharing limiter until the ban ends
		}
		desired[u] = true
	}
	var add, remove []string
	for u := range desired {
		add = append(add, u)
	}
	for u := range m.known {
		if !desired[u] {
			remove = append(remove, u)
		}
	}
	for _, u := range uuids {
		m.known[u] = true
	}
	m.mu.Unlock()

	for _, u := range remove {
		if err := m.api.RemoveUser(ctx, u); err != nil {
			m.log.Warn("xray live apply failed; restart the xray container to apply (docker compose restart xray)", "error", err)
			return
		}
	}
	for _, u := range add {
		if err := m.api.AddUser(ctx, u); err != nil {
			m.log.Warn("xray live apply failed; restart the xray container to apply (docker compose restart xray)", "error", err)
			return
		}
	}
}
