package main

// Agent core: keys, registration, the sync tick and peer application. The
// WireGuard control is behind the wgLayer interface so the whole agent runs
// in passive mode (pure logic) on any platform, including CI.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fenjo26/Fibr/internal/node"
	"github.com/fenjo26/Fibr/internal/xray"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// wgLayer abstracts the local WireGuard interface so the agent logic is
// testable without a kernel interface (passive layer logs instead).
type wgLayer interface {
	// EnsureInterface creates the interface (if missing), assigns the tunnel
	// addresses delivered by the control plane and installs NAT rules.
	// Empty addresses mean "sync not done yet" — create-only.
	EnsureInterface(serverIPv4, serverIPv6 string) error
	// Apply replaces the whole peer set; an empty slice clears all peers.
	Apply(peers []node.Peer) error
	// Stats reads live counters for the currently applied peers.
	Stats(applied []node.Peer) []node.PeerStats
}

// Agent is the node runtime.
type Agent struct {
	cfg    Config
	log    *slog.Logger
	client *http.Client

	priv     wgtypes.Key // this node's WireGuard identity
	xrayPriv string      // Reality private key (b64url), empty when xray off
	xrayPub  string
	xraySID  string

	wg           wgLayer
	revision     int64       // last applied peer revision
	applied      []node.Peer // last applied peer set (drives stats reporting)
	appliedUUIDs []string
	xrayWritten  string // last written xray config (avoid useless rewrites)
}

// NewAgent loads (or generates) the node's key material and wires the real
// or passive WireGuard layer.
func NewAgent(cfg Config, log *slog.Logger) (*Agent, error) {
	priv, xpriv, xpub, xsid, err := LoadKeys(cfg.DataDir, cfg.XrayEnabled)
	if err != nil {
		return nil, err
	}
	ag := &Agent{
		cfg: cfg, log: log, client: httpClient(),
		priv: priv, xrayPriv: xpriv, xrayPub: xpub, xraySID: xsid,
	}
	ag.revision = readRevision(cfg.DataDir)

	var client *wgctrl.Client
	if !cfg.Passive {
		if c, err := wgctrl.New(); err != nil {
			log.Warn("WireGuard control plane unavailable, running passive", "error", err)
		} else {
			client = c
		}
	}
	if client == nil || runtime.GOOS != "linux" {
		ag.wg = &passiveLayer{log: log}
	} else {
		ag.wg = &kernelLayer{client: client, cfg: cfg, priv: priv, log: log}
	}
	return ag, nil
}

/* ---------------------------- keys ---------------------------- */

// LoadKeys loads or generates the node's WireGuard keypair (<data>/node.key)
// and, when xray is enabled, the Reality x25519 keypair and short id
// (<data>/reality.key, <data>/reality.sid). Keys are write-once: the
// control plane refuses a changed key (409) because issued client configs
// embed it.
func LoadKeys(dataDir string, xrayEnabled bool) (priv wgtypes.Key, xrayPriv, xrayPub, xraySID string, err error) {
	keyPath := filepath.Join(dataDir, "node.key")
	raw, rerr := os.ReadFile(keyPath)
	switch {
	case rerr == nil:
		priv, err = wgtypes.ParseKey(string(bytes.TrimSpace(raw)))
		if err != nil {
			return priv, "", "", "", fmt.Errorf("stored node key is invalid: %w", err)
		}
	case os.IsNotExist(rerr):
		priv, err = wgtypes.GeneratePrivateKey()
		if err != nil {
			return priv, "", "", "", fmt.Errorf("generate node key: %w", err)
		}
		if err = os.WriteFile(keyPath, []byte(priv.String()), 0o600); err != nil {
			return priv, "", "", "", fmt.Errorf("persist node key: %w", err)
		}
	default:
		return priv, "", "", "", rerr
	}
	if !xrayEnabled {
		return priv, "", "", "", nil
	}

	rkPath := filepath.Join(dataDir, "reality.key")
	rkRaw, rkErr := os.ReadFile(rkPath)
	switch {
	case rkErr == nil:
		xrayPriv = string(bytes.TrimSpace(rkRaw))
		// Re-derive the public half from a private-only file would need the
		// ecdh API; the agent stores both halves for simplicity — pub first.
		pubPath := filepath.Join(dataDir, "reality.pub")
		pubRaw, perr := os.ReadFile(pubPath)
		if perr != nil {
			return priv, "", "", "", fmt.Errorf("reality.pub missing next to reality.key: %w", perr)
		}
		xrayPub = string(bytes.TrimSpace(pubRaw))
	case os.IsNotExist(rkErr):
		var gerr error
		xrayPriv, xrayPub, gerr = xray.GenerateRealityKeypair()
		if gerr != nil {
			return priv, "", "", "", fmt.Errorf("generate reality key: %w", gerr)
		}
		if gerr = os.WriteFile(rkPath, []byte(xrayPriv), 0o600); gerr != nil {
			return priv, "", "", "", gerr
		}
		if gerr = os.WriteFile(filepath.Join(dataDir, "reality.pub"), []byte(xrayPub), 0o644); gerr != nil {
			return priv, "", "", "", gerr
		}
	default:
		return priv, "", "", "", rkErr
	}

	sidPath := filepath.Join(dataDir, "reality.sid")
	sidRaw, serr := os.ReadFile(sidPath)
	switch {
	case serr == nil:
		xraySID = string(bytes.TrimSpace(sidRaw))
	case os.IsNotExist(serr):
		b := make([]byte, 4) // 8 hex chars, same width as the control plane's
		if _, serr = rand.Read(b); serr != nil {
			return priv, "", "", "", serr
		}
		xraySID = hex.EncodeToString(b)
		if serr = os.WriteFile(sidPath, []byte(xraySID), 0o644); serr != nil {
			return priv, "", "", "", serr
		}
	default:
		return priv, "", "", "", serr
	}
	return priv, xrayPriv, xrayPub, xraySID, nil
}

/* ---------------------------- http ---------------------------- */

// Register posts this node's public keys to the control plane. It retries
// with backoff until it gets a 2xx; 409 (key mismatch) is fatal — the node
// was re-created server-side and the old identity is dead.
func (ag *Agent) Register(ctx context.Context) error {
	body := node.RegisterRequest{ServerPublicKey: ag.priv.PublicKey().String()}
	if ag.cfg.XrayEnabled {
		body.XrayRealityPublicKey = &ag.xrayPub
		body.XrayShortID = &ag.xraySID
	}
	backoff := time.Second
	for attempt := 0; ; attempt++ {
		code, _, err := ag.post(ctx, "/api/node/register", body)
		switch {
		case err == nil && code >= 200 && code < 300:
			return nil
		case err == nil && code == http.StatusConflict:
			return errors.New("control plane reports key mismatch: delete and re-create this node, then wipe FIBR_DATA_DIR")
		case ctx.Err() != nil:
			return ctx.Err()
		default:
			if attempt == 0 {
				ag.log.Warn("register failed, retrying", "status", code, "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// Tick runs one sync cycle: report stats, pull the desired state, apply it.
func (ag *Agent) Tick(ctx context.Context) error {
	stats := ag.wg.Stats(ag.applied)
	resp, err := ag.sync(ctx, stats)
	if err != nil {
		return err
	}
	ag.applySync(resp)
	return nil
}

func (ag *Agent) sync(ctx context.Context, stats []node.PeerStats) (*node.SyncResponse, error) {
	if stats == nil {
		stats = []node.PeerStats{}
	}
	code, body, err := ag.post(ctx, "/api/node/sync", node.SyncRequest{Revision: ag.revision, Stats: stats})
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("sync: status %d: %s", code, body)
	}
	var out node.SyncResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("sync: bad response: %w", err)
	}
	return &out, nil
}

func (ag *Agent) post(ctx context.Context, path string, body any) (int, string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ag.cfg.APIURL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ag.cfg.Token)
	resp, err := ag.client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(data), nil
}

/* ---------------------------- apply ---------------------------- */

// applySync converges the local state to the control plane's answer:
// blocked clears everything; otherwise peers (and the xray config) are
// applied when the revision advanced or the content differs. The revision is
// persisted so a restart does not re-apply an unchanged set needlessly —
// though applying is idempotent anyway.
func (ag *Agent) applySync(resp *node.SyncResponse) {
	if resp == nil {
		return
	}
	if resp.Status == "blocked" {
		if len(ag.applied) > 0 {
			if err := ag.wg.Apply(nil); err != nil {
				ag.log.Error("cannot clear peers on blocked node", "error", err)
			} else {
				ag.log.Warn("node blocked: all peers removed")
			}
		}
		ag.applied = nil
		ag.appliedUUIDs = nil
		ag.xrayWriteIfChanged(resp, nil)
		ag.setRevision(resp.Revision)
		return
	}

	peersChanged := resp.Revision != ag.revision || !peersEqual(resp.Peers, ag.applied)
	if peersChanged {
		if err := ag.wg.Apply(resp.Peers); err != nil {
			ag.log.Error("cannot apply peers", "count", len(resp.Peers), "error", err)
			// Do not record the revision: retry on the next tick.
			return
		}
		ag.applied = append(ag.applied[:0], resp.Peers...)
		ag.log.Info("peers applied", "count", len(resp.Peers), "revision", resp.Revision)
	}

	if resp.ServerIPv4 != "" {
		// Addresses only arrive with a successful sync; re-running the
		// interface setup is idempotent (ip addr replace).
		if err := ag.wg.EnsureInterface(resp.ServerIPv4, resp.ServerIPv6); err != nil {
			ag.log.Warn("interface address refresh failed", "error", err)
		}
	}

	ag.xrayWriteIfChanged(resp, resp.XrayUUIDs)
	ag.setRevision(resp.Revision)
}

// xrayWriteIfChanged renders the xray server config (this node's own Reality
// key and short id) when the uuid set changed. Written atomically, same
// pattern as the control plane's xray.Manager.SyncClients.
func (ag *Agent) xrayWriteIfChanged(resp *node.SyncResponse, uuids []string) {
	if !ag.cfg.XrayEnabled || ag.xrayPriv == "" {
		return
	}
	data, err := xray.BuildStandaloneConfig(ag.xrayListenPort(), resp.XraySNI, resp.XrayDest, ag.xrayPriv, ag.xraySID, uuids)
	if err != nil {
		ag.log.Error("cannot build xray config", "error", err)
		return
	}
	if string(data) == ag.xrayWritten {
		return
	}
	path := filepath.Join(ag.cfg.DataDir, "xray", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		ag.log.Error("cannot create xray dir", "error", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		ag.log.Error("cannot write xray config", "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		ag.log.Error("cannot swap xray config", "error", err)
		return
	}
	ag.xrayWritten = string(data)
	ag.appliedUUIDs = append(ag.appliedUUIDs[:0], uuids...)
	ag.log.Info("xray config written; restart the xray container to apply", "clients", len(uuids), "path", path)
}

// xrayListenPort returns the port the local xray container serves on. The
// node row's xray_port is what the control plane puts into client configs,
// so the operator must keep the two in sync; the agent reads its own env
// (FIBR_XRAY_PORT, default 8443 — the control-plane default).
func (ag *Agent) xrayListenPort() int {
	return ag.cfg.XrayPort
}

func (ag *Agent) setRevision(v int64) {
	if v == ag.revision {
		return
	}
	ag.revision = v
	if err := os.WriteFile(filepath.Join(ag.cfg.DataDir, "revision"), []byte(strconv.FormatInt(v, 10)), 0o644); err != nil {
		ag.log.Warn("cannot persist revision", "error", err)
	}
}

func readRevision(dataDir string) int64 {
	raw, err := os.ReadFile(filepath.Join(dataDir, "revision"))
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

/* ---------------------------- pure helpers ---------------------------- */

// peersEqual reports whether two peer sets describe the same configuration
// (order-insensitive). Used to skip no-op interface rewrites.
func peersEqual(a, b []node.Peer) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(p node.Peer) string {
		ips := append([]string(nil), p.AllowedIPs...)
		sort.Strings(ips)
		return fmt.Sprintf("%d|%s|%s|%v", p.DeviceID, p.PublicKey, p.PresharedKey, ips)
	}
	seen := make(map[string]int, len(a))
	for _, p := range a {
		seen[key(p)]++
	}
	for _, p := range b {
		k := key(p)
		if seen[k] == 0 {
			return false
		}
		seen[k]--
	}
	return true
}

/* ---------------------------- passive layer ---------------------------- */

// passiveLayer is the no-op WireGuard layer (FIBR_PASSIVE=1, dev machines,
// CI): it logs what would be applied and reports no stats.
type passiveLayer struct{ log *slog.Logger }

func (p *passiveLayer) EnsureInterface(v4, v6 string) error {
	p.log.Info("passive: interface setup skipped", "ipv4", v4, "ipv6", v6)
	return nil
}

func (p *passiveLayer) Apply(peers []node.Peer) error {
	p.log.Info("passive: would apply peers", "count", len(peers))
	for _, peer := range peers {
		p.log.Info("passive: peer", "device_id", peer.DeviceID, "public_key", peer.PublicKey, "allowed_ips", peer.AllowedIPs)
	}
	return nil
}

func (p *passiveLayer) Stats(_ []node.Peer) []node.PeerStats { return []node.PeerStats{} }

/* ---------------------------- kernel layer ---------------------------- */

// kernelLayer drives a real WireGuard interface with wgctrl + iproute2
// (mirrors internal/wg's patterns).
type kernelLayer struct {
	client *wgctrl.Client
	cfg    Config
	priv   wgtypes.Key
	log    *slog.Logger
}

func (k *kernelLayer) EnsureInterface(serverIPv4, serverIPv6 string) error {
	// wgctrl (this version) has no link creation — iproute2 creates the
	// interface, same as the control plane's internal/wg.
	if err := run("ip", "link", "show", k.cfg.Iface); err != nil {
		if err := run("ip", "link", "add", k.cfg.Iface, "type", "wireguard"); err != nil {
			return fmt.Errorf("create %s: %w", k.cfg.Iface, err)
		}
	}
	if serverIPv4 == "" {
		return nil // addresses arrive with the first sync
	}
	if err := run("ip", "addr", "replace", serverIPv4+"/24", "dev", k.cfg.Iface); err != nil {
		return fmt.Errorf("assign address: %w", err)
	}
	if serverIPv6 != "" {
		if err := run("ip", "addr", "replace", serverIPv6+"/64", "dev", k.cfg.Iface); err != nil {
			return fmt.Errorf("assign ipv6 address: %w", err)
		}
	}
	if err := run("ip", "link", "set", "mtu", strconv.Itoa(k.cfg.MTU), "dev", k.cfg.Iface, "up"); err != nil {
		return fmt.Errorf("bring up: %w", err)
	}
	port := k.cfg.ListenPort
	priv := k.priv
	if err := k.client.ConfigureDevice(k.cfg.Iface, wgtypes.Config{
		PrivateKey:   &priv,
		ListenPort:   &port,
		ReplacePeers: false,
	}); err != nil {
		return fmt.Errorf("configure device: %w", err)
	}
	return k.ensureFirewall(serverIPv4, serverIPv6)
}

// ensureFirewall installs NAT/forwarding (idempotent, best-effort — mirrors
// internal/wg.Manager.ensureFirewall).
func (k *kernelLayer) ensureFirewall(serverIPv4, serverIPv6 string) error {
	if _, err := exec.LookPath("iptables"); err != nil {
		k.log.Warn("iptables not found, skipping firewall rules")
		return nil
	}
	_, v4net, err := net.ParseCIDR(serverIPv4 + "/24")
	if err != nil {
		return nil
	}
	ext := detectDefaultIface()
	if ext == "" {
		ext = "eth0"
	}
	type rule struct{ check, add []string }
	rules := []rule{
		{
			check: []string{"-t", "nat", "-C", "POSTROUTING", "-s", v4net.String(), "-o", ext, "-j", "MASQUERADE"},
			add:   []string{"-t", "nat", "-A", "POSTROUTING", "-s", v4net.String(), "-o", ext, "-j", "MASQUERADE"},
		},
		{
			check: []string{"-C", "FORWARD", "-i", k.cfg.Iface, "-j", "ACCEPT"},
			add:   []string{"-A", "FORWARD", "-i", k.cfg.Iface, "-j", "ACCEPT"},
		},
		{
			check: []string{"-C", "FORWARD", "-o", k.cfg.Iface, "-j", "ACCEPT"},
			add:   []string{"-A", "FORWARD", "-o", k.cfg.Iface, "-j", "ACCEPT"},
		},
	}
	for _, r := range rules {
		if err := run("iptables", r.check...); err == nil {
			continue
		}
		if err := run("iptables", r.add...); err != nil {
			return fmt.Errorf("iptables %v: %w", r.add, err)
		}
	}
	if serverIPv6 != "" {
		if _, err := exec.LookPath("ip6tables"); err == nil {
			v6check := []string{"-t", "nat", "-C", "POSTROUTING", "-s", maskV6(serverIPv6), "-o", ext, "-j", "MASQUERADE"}
			if err := run("ip6tables", v6check...); err != nil {
				v6add := []string{"-t", "nat", "-A", "POSTROUTING", "-s", maskV6(serverIPv6), "-o", ext, "-j", "MASQUERADE"}
				if err := run("ip6tables", v6add...); err != nil {
					k.log.Warn("ip6tables MASQUERADE failed (IPv6 egress disabled)", "error", err)
				}
			}
		}
	}
	return nil
}

// maskV6 turns fd42:f1b2::1 into fd42:f1b2::/64 for rule matching.
func maskV6(ip string) string {
	parsed := net.ParseIP(ip).To16()
	if parsed == nil {
		return ip
	}
	out := make(net.IP, net.IPv6len)
	copy(out, parsed)
	for i := 8; i < 16; i++ {
		out[i] = 0
	}
	_, ipnet, err := net.ParseCIDR(out.String() + "/64")
	if err != nil {
		return ip
	}
	return ipnet.String()
}

// Apply replaces the whole peer set on the interface.
func (k *kernelLayer) Apply(peers []node.Peer) error {
	cfgs := make([]wgtypes.PeerConfig, 0, len(peers))
	ka := 25 * time.Second
	for _, p := range peers {
		pub, err := wgtypes.ParseKey(p.PublicKey)
		if err != nil {
			k.log.Error("bad public key from control plane, skipping", "device_id", p.DeviceID, "error", err)
			continue
		}
		var psk *wgtypes.Key
		if p.PresharedKey != "" {
			pk, err := wgtypes.ParseKey(p.PresharedKey)
			if err != nil {
				k.log.Error("bad preshared key from control plane, skipping", "device_id", p.DeviceID, "error", err)
				continue
			}
			psk = &pk
		}
		allowed := make([]net.IPNet, 0, len(p.AllowedIPs))
		for _, cidr := range p.AllowedIPs {
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				k.log.Error("bad allowed ip from control plane, skipping", "cidr", cidr)
				continue
			}
			allowed = append(allowed, *ipnet)
		}
		cfgs = append(cfgs, wgtypes.PeerConfig{
			PublicKey:                   pub,
			PresharedKey:                psk,
			AllowedIPs:                  allowed,
			PersistentKeepaliveInterval: &ka,
		})
	}
	return k.client.ConfigureDevice(k.cfg.Iface, wgtypes.Config{
		ReplacePeers: true,
		Peers:        cfgs,
	})
}

// Stats maps the interface's live counters onto device ids via the applied
// peer set.
func (k *kernelLayer) Stats(applied []node.Peer) []node.PeerStats {
	dev, err := k.client.Device(k.cfg.Iface)
	if err != nil {
		return []node.PeerStats{}
	}
	idOf := make(map[string]int64, len(applied))
	for _, p := range applied {
		idOf[p.PublicKey] = p.DeviceID
	}
	out := make([]node.PeerStats, 0, len(dev.Peers))
	for _, p := range dev.Peers {
		id, ok := idOf[p.PublicKey.String()]
		if !ok {
			continue
		}
		var hs int64
		if !p.LastHandshakeTime.IsZero() {
			hs = p.LastHandshakeTime.Unix()
		}
		out = append(out, node.PeerStats{
			DeviceID:      id,
			LastHandshake: hs,
			Rx:            p.ReceiveBytes,
			Tx:            p.TransmitBytes,
		})
	}
	return out
}

func detectDefaultIface() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	fields := bytes.Fields(out)
	for i, f := range fields {
		if string(f) == "dev" && i+1 < len(fields) {
			return strings.TrimSpace(string(fields[i+1]))
		}
	}
	return ""
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return nil
}
