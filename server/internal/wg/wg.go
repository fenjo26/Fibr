// Package wg manages the local WireGuard interface: interface lifecycle,
// peer sync with the database, transfer stats and client config rendering.
//
// On hosts where kernel WireGuard / iproute2 / wgctrl are unavailable (macOS
// dev machine, CI) the Manager degrades to passive mode: it still generates
// keys and renders client configs, but skips interface and peer control.
package wg

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fenjo26/Fibr/internal/awg"
	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/node"
	"github.com/fenjo26/Fibr/internal/store"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const settingServerKey = "wg_server_private_key"

// Manager owns everything WireGuard-related.
type Manager struct {
	cfg   *config.Config
	store *store.Store
	log   *slog.Logger

	client   *wgctrl.Client // nil in passive mode
	privKey  wgtypes.Key
	pubKey   wgtypes.Key
	subnet   *net.IPNet
	extIface string
	awg      *awg.Params // non-nil when FIBR_WG_ENGINE=awg
	serves   string      // device protocol this tunnel carries: "wg" or "awg"

	mu          sync.Mutex
	samples     []ifaceSample
	speeds      map[int64]PeerSpeed // last per-device rates; nil until 2nd sample
	lastStatsAt time.Time           // zero until the first CollectStats

	dnsOn atomic.Bool // the protected resolver started; configs point DNS at the gateway
}

// SetDNSResolver flips the client-config DNS line: while the protected
// resolver runs (internal/dns), configs of devices with a filtering profile
// get "DNS = <tunnel gateway>" instead of the plain FIBR_DNS resolvers
// (docs/dns-games.md §6.8). Called from main once, before the API serves.
func (m *Manager) SetDNSResolver(on bool) { m.dnsOn.Store(on) }

// DNSLine picks the resolvers written into a client config: the tunnel
// gateway while the protected resolver runs and the device's effective
// profile is not "off", otherwise the plain public resolvers.
func (m *Manager) DNSLine(d store.Device) string {
	profile := d.DNSProfile
	if profile == "" {
		profile = m.cfg.DNSDefaultProfile
	}
	if m.dnsOn.Load() && profile != "" && profile != "off" {
		return m.cfg.ServerIP
	}
	return strings.Join(m.cfg.DNS, ", ")
}

type ifaceSample struct {
	t            time.Time
	wgRx, wgTx   int64
	extRx, extTx int64
}

// New creates the manager, loading or generating the server keypair.
func New(cfg *config.Config, st *store.Store, log *slog.Logger) (*Manager, error) {
	_, ipnet, err := net.ParseCIDR(cfg.Subnet)
	if err != nil {
		return nil, fmt.Errorf("subnet: %w", err)
	}

	m := &Manager{cfg: cfg, store: st, log: log, subnet: ipnet, extIface: "eth0", serves: store.ProtocolWG}

	// Server keypair lives in the settings table, generated once.
	privStr, err := st.GetSetting(settingServerKey)
	switch {
	case err == nil:
		k, perr := wgtypes.ParseKey(privStr)
		if perr != nil {
			return nil, fmt.Errorf("stored server key is invalid: %w", perr)
		}
		m.privKey = k
	case err == store.ErrNotFound:
		k, gerr := wgtypes.GeneratePrivateKey()
		if gerr != nil {
			return nil, fmt.Errorf("generate server key: %w", gerr)
		}
		if serr := st.SetSetting(settingServerKey, k.String()); serr != nil {
			return nil, fmt.Errorf("persist server key: %w", serr)
		}
		m.privKey = k
	default:
		return nil, err
	}
	m.pubKey = m.privKey.PublicKey()

	// AmneziaWG obfuscation parameters are a property of the whole server
	// (they live on the interface, not per peer), generated once and shared
	// by every client config.
	if cfg.WGEngine == config.EngineAWG {
		p, err := awg.LoadOrGenerateParams(st)
		if err != nil {
			return nil, fmt.Errorf("awg params: %w", err)
		}
		m.awg = p
	}

	if c, err := wgctrl.New(); err != nil {
		log.Warn("WireGuard control plane unavailable, running in passive mode (configs only, no interface)", "error", err)
	} else {
		m.client = c
	}
	if ext := detectDefaultIface(); ext != "" {
		m.extIface = ext
	}
	return m, nil
}

// NewAWG creates the manager of the second, AmneziaWG tunnel: the devices
// with protocol "awg" on cfg.AWGConfig()'s interface/port/subnet. It shares
// the server keypair with the main tunnel (same settings row).
func NewAWG(base *config.Config, st *store.Store, log *slog.Logger) (*Manager, error) {
	cfg, err := base.AWGConfig()
	if err != nil {
		return nil, err
	}
	m, err := New(cfg, st, log.With("tunnel", cfg.WGInterface))
	if err != nil {
		return nil, err
	}
	m.serves = store.ProtocolAWG
	return m, nil
}

// Config is the tunnel's own configuration (the AWG tunnel's differs).
func (m *Manager) Config() *config.Config { return m.cfg }

// PublicKey returns the server's WireGuard public key.
func (m *Manager) PublicKey() string { return m.pubKey.String() }

// GeneratePresharedKey returns a fresh random preshared key (wgtypes has no
// generator for these).
func GeneratePresharedKey() (wgtypes.Key, error) {
	var k wgtypes.Key
	if _, err := rand.Read(k[:]); err != nil {
		return k, err
	}
	return k, nil
}

// Passive reports whether live WireGuard control is unavailable or the
// platform cannot host the kernel interface.
func (m *Manager) Passive() bool { return m.client == nil || runtime.GOOS != "linux" }

// EnsureInterface creates and configures wg0 (Linux + wgctrl only).
func (m *Manager) EnsureInterface() error {
	if m.client == nil || runtime.GOOS != "linux" {
		m.log.Warn("skipping WireGuard interface setup (passive/dev mode)")
		return nil
	}
	if m.cfg.WGEngine == config.EngineAWG {
		return m.ensureInterfaceAWG()
	}

	if err := run("ip", "link", "show", m.cfg.WGInterface); err != nil {
		if err := run("ip", "link", "add", m.cfg.WGInterface, "type", "wireguard"); err != nil {
			return fmt.Errorf("create %s: %w", m.cfg.WGInterface, err)
		}
	}
	if err := run("ip", "addr", "replace", m.cfg.ServerIP+"/24", "dev", m.cfg.WGInterface); err != nil {
		return fmt.Errorf("assign address: %w", err)
	}
	if err := run("ip", "addr", "replace", m.cfg.ServerIP6+"/64", "dev", m.cfg.WGInterface); err != nil {
		return fmt.Errorf("assign ipv6 address: %w", err)
	}
	if err := run("ip", "link", "set", "mtu", strconv.Itoa(m.cfg.MTU), "dev", m.cfg.WGInterface, "up"); err != nil {
		return fmt.Errorf("bring up: %w", err)
	}

	port := m.cfg.WGPort
	priv := m.privKey
	if err := m.client.ConfigureDevice(m.cfg.WGInterface, wgtypes.Config{
		PrivateKey:   &priv,
		ListenPort:   &port,
		ReplacePeers: false,
	}); err != nil {
		return fmt.Errorf("configure device: %w", err)
	}
	return m.ensureFirewall()
}

// ensureInterfaceAWG configures the interface owned by the amneziawg-go
// userspace daemon. The daemon creates the TUN link itself (started by
// cmd/fibr before the manager), so unlike kernel mode there is no
// `ip link add` here — but address, MTU, firewall and the crypto/peer
// configuration are managed exactly the same. wgctrl reaches the daemon's
// UAPI socket through the /var/run/wireguard symlink (see internal/awg).
func (m *Manager) ensureInterfaceAWG() error {
	if err := run("ip", "link", "show", m.cfg.WGInterface); err != nil {
		return fmt.Errorf("%s does not exist: the amneziawg-go daemon must create it (is it running?): %w", m.cfg.WGInterface, err)
	}
	if err := run("ip", "addr", "replace", m.cfg.ServerIP+"/24", "dev", m.cfg.WGInterface); err != nil {
		return fmt.Errorf("assign address: %w", err)
	}
	if err := run("ip", "addr", "replace", m.cfg.ServerIP6+"/64", "dev", m.cfg.WGInterface); err != nil {
		return fmt.Errorf("assign ipv6 address: %w", err)
	}
	if err := run("ip", "link", "set", "mtu", strconv.Itoa(m.cfg.MTU), "dev", m.cfg.WGInterface, "up"); err != nil {
		return fmt.Errorf("bring up: %w", err)
	}

	port := m.cfg.WGPort
	priv := m.privKey
	if err := m.client.ConfigureDevice(m.cfg.WGInterface, wgtypes.Config{
		PrivateKey:   &priv,
		ListenPort:   &port,
		ReplacePeers: false,
	}); err != nil {
		return fmt.Errorf("configure device via uapi: %w (check the /var/run/wireguard symlink to /var/run/amneziawg)", err)
	}
	if err := awg.ApplyParams(m.cfg.WGInterface, m.awg); err != nil {
		return fmt.Errorf("apply amneziawg params: %w", err)
	}
	m.log.Info("amneziawg obfuscation applied",
		"jc", m.awg.Jc, "jmin", m.awg.Jmin, "jmax", m.awg.Jmax,
		"s1", m.awg.S1, "s2", m.awg.S2)
	return m.ensureFirewall()
}

// ensureFirewall installs NAT and forwarding rules (idempotent).
func (m *Manager) ensureFirewall() error {
	if _, err := exec.LookPath("iptables"); err != nil {
		m.log.Warn("iptables not found, skipping firewall rules")
		return nil
	}
	type rule struct{ check, add []string }
	rules := []rule{
		{
			check: []string{"-t", "nat", "-C", "POSTROUTING", "-s", m.subnet.String(), "-o", m.extIface, "-j", "MASQUERADE"},
			add:   []string{"-t", "nat", "-A", "POSTROUTING", "-s", m.subnet.String(), "-o", m.extIface, "-j", "MASQUERADE"},
		},
		{
			check: []string{"-C", "FORWARD", "-i", m.cfg.WGInterface, "-j", "ACCEPT"},
			add:   []string{"-A", "FORWARD", "-i", m.cfg.WGInterface, "-j", "ACCEPT"},
		},
		{
			check: []string{"-C", "FORWARD", "-o", m.cfg.WGInterface, "-j", "ACCEPT"},
			add:   []string{"-A", "FORWARD", "-o", m.cfg.WGInterface, "-j", "ACCEPT"},
		},
	}
	for _, r := range rules {
		if err := run("iptables", r.check...); err == nil {
			continue // rule already present
		}
		if err := run("iptables", r.add...); err != nil {
			return fmt.Errorf("iptables %v: %w", r.add, err)
		}
	}

	// Dual-stack egress is best-effort: hosts without IPv6 (or without
	// ip6tables) simply never route v6 traffic out; v4 keeps working.
	if _, err := exec.LookPath("ip6tables"); err == nil {
		v6check := []string{"-t", "nat", "-C", "POSTROUTING", "-s", m.cfg.Subnet6, "-o", m.extIface, "-j", "MASQUERADE"}
		if err := run("ip6tables", v6check...); err != nil {
			v6add := []string{"-t", "nat", "-A", "POSTROUTING", "-s", m.cfg.Subnet6, "-o", m.extIface, "-j", "MASQUERADE"}
			if err := run("ip6tables", v6add...); err != nil {
				m.log.Warn("ip6tables MASQUERADE failed (IPv6 egress disabled)", "error", err)
			}
		}
	}
	return nil
}

// SyncPeers rebuilds the peer list on the interface from the database.
// Re-configuring unchanged peers does not drop their sessions.
func (m *Manager) SyncPeers() error {
	if m.client == nil || runtime.GOOS != "linux" {
		return nil
	}
	// Active devices only: the store query drops disabled devices and the
	// devices of users (or owner-admins) past their subscription deadline,
	// which is what actually cuts an expired account's tunnel (see
	// docs/billing.md).
	devices, err := m.store.ListActiveDevices(time.Now())
	if err != nil {
		return err
	}
	peers := make([]wgtypes.PeerConfig, 0, len(devices))
	for _, d := range devices {
		// Each tunnel carries its own devices: "wg" ones on the main
		// interface, "awg" ones on the AmneziaWG tunnel. Xray devices reuse
		// the key columns for a UUID (see store.Device) and are served by a
		// separate xray container.
		proto := d.Protocol
		if proto == "" {
			proto = store.ProtocolWG
		}
		if proto != m.serves {
			continue
		}
		pub, err := wgtypes.ParseKey(d.PublicKey)
		if err != nil {
			m.log.Error("bad public key in db, skipping device", "device_id", d.ID, "error", err)
			continue
		}
		psk, err := wgtypes.ParseKey(d.PresharedKey)
		if err != nil {
			m.log.Error("bad preshared key in db, skipping device", "device_id", d.ID, "error", err)
			continue
		}
		ip := net.ParseIP(d.IPv4).To4()
		if ip == nil {
			continue
		}
		// Kernel WireGuard silently ignores keepalives for peers whose
		// endpoint is not yet known (road warriors until their first
		// handshake). The userspace daemon instead logs an error per attempt,
		// so in awg mode the server does not push keepalives: the client
		// configs carry PersistentKeepalive=25 anyway, which is what keeps
		// the NAT mapping alive in both directions.
		var ka *time.Duration
		if m.cfg.WGEngine != config.EngineAWG {
			v := 25 * time.Second
			ka = &v
		}
		// Dual-stack: the peer's tunnel IPs are its /32 and the derived /128.
		allowed := []net.IPNet{{IP: ip, Mask: net.CIDRMask(32, 32)}}
		if ip6 := net.ParseIP(m.cfg.IPv6For(d.IPv4)); ip6 != nil {
			allowed = append(allowed, net.IPNet{IP: ip6.To16(), Mask: net.CIDRMask(128, 128)})
		}
		peers = append(peers, wgtypes.PeerConfig{
			PublicKey:                   pub,
			PresharedKey:                &psk,
			AllowedIPs:                  allowed,
			PersistentKeepaliveInterval: ka,
		})
	}
	priv := m.privKey
	port := m.cfg.WGPort
	err = m.client.ConfigureDevice(m.cfg.WGInterface, wgtypes.Config{
		PrivateKey:   &priv,
		ListenPort:   &port,
		ReplacePeers: true,
		Peers:        peers,
	})
	if err != nil {
		m.log.Error("peer sync failed", "error", err)
		return err
	}
	m.log.Info("peers synced", "count", len(peers))
	return nil
}

// PeerSpeed is the current transfer rate of one device, computed from the
// delta between the two most recent stats samples.
type PeerSpeed struct {
	RxBps float64
	TxBps float64
}

// CollectStats reads peer transfer counters into the database and records an
// interface throughput sample. Online = handshake in the last 3 minutes.
// Per-device speeds are refreshed here too (DeviceSpeeds).
func (m *Manager) CollectStats() {
	now := time.Now()
	if m.client != nil && runtime.GOOS == "linux" {
		dev, err := m.client.Device(m.cfg.WGInterface)
		if err != nil {
			m.log.Warn("stats: cannot read device", "error", err)
		} else {
			speeds := make(map[int64]PeerSpeed, len(dev.Peers))
			// Speeds need a previous sample to compare against; the very
			// first pass only records counters (otherwise a restart would
			// show a huge spike from all historical traffic).
			var dt float64
			if !m.lastStatsAt.IsZero() {
				dt = now.Sub(m.lastStatsAt).Seconds()
			}
			for _, p := range dev.Peers {
				d, err := m.store.GetDeviceByKey(p.PublicKey.String())
				if err != nil {
					continue // foreign peer, not tracked
				}
				rx, tx := p.ReceiveBytes, p.TransmitBytes
				dRx, dTx := rx-d.LastRx, tx-d.LastTx
				if dRx < 0 {
					dRx = rx // counters were reset (interface restart)
				}
				if dTx < 0 {
					dTx = tx
				}
				if dt > 0 {
					speeds[d.ID] = PeerSpeed{
						RxBps: float64(dRx) * 8 / dt,
						TxBps: float64(dTx) * 8 / dt,
					}
				}
				if err := m.store.UpdateDeviceStats(d.ID, p.LastHandshakeTime, rx, tx, d.TotalRx+dRx, d.TotalTx+dTx); err != nil {
					m.log.Warn("stats: cannot update device", "device_id", d.ID, "error", err)
				}
			}
			m.mu.Lock()
			if dt > 0 {
				m.speeds = speeds
			}
			m.lastStatsAt = now
			m.mu.Unlock()
		}
	}
	m.sampleIfaces()
}

// Throughput returns current bits-per-second over the tunnel and the external
// interface, computed from the last two samples.
// DeviceSpeeds returns a copy of the latest per-device transfer rates
// (empty until the second stats sample; xray devices are never listed).
func (m *Manager) DeviceSpeeds() map[int64]PeerSpeed {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int64]PeerSpeed, len(m.speeds))
	for k, v := range m.speeds {
		out[k] = v
	}
	return out
}

func (m *Manager) Throughput() (wgIn, wgOut, extIn, extOut float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.samples) < 2 {
		return 0, 0, 0, 0
	}
	a, b := m.samples[len(m.samples)-2], m.samples[len(m.samples)-1]
	dt := b.t.Sub(a.t).Seconds()
	if dt <= 0 {
		return 0, 0, 0, 0
	}
	per := func(cur, prev int64) float64 {
		d := cur - prev
		if d < 0 {
			d = 0
		}
		return float64(d) * 8 / dt
	}
	return per(b.wgRx, a.wgRx), per(b.wgTx, a.wgTx), per(b.extRx, a.extRx), per(b.extTx, a.extTx)
}

func (m *Manager) sampleIfaces() {
	wgRx, wgTx := readIfaces(m.cfg.WGInterface)
	extRx, extTx := readIfaces(m.extIface)
	m.mu.Lock()
	m.samples = append(m.samples, ifaceSample{time.Now(), wgRx, wgTx, extRx, extTx})
	if len(m.samples) > 12 {
		m.samples = m.samples[len(m.samples)-12:]
	}
	m.mu.Unlock()
}

// readIfaces sums rx/tx bytes of the named interface from /proc/net/dev.
// Returns zeros when unavailable (macOS dev mode).
func readIfaces(name string) (rx, tx int64) {
	out, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.Fields(line)
		iface := strings.TrimSuffix(parts[0], ":")
		if iface != name {
			continue
		}
		// iface: rx_bytes rx_packets ... tx_bytes ... — tx_bytes is field 9 (index 9)
		if len(parts) < 10 {
			return 0, 0
		}
		rx = parseInt(parts[1])
		tx = parseInt(parts[9])
		return rx, tx
	}
	return 0, 0
}

// ClientConfig renders a wg-quick-compatible config for the device against
// this box's own endpoint (the single-node behaviour). In awg engine mode
// the [Interface] section carries the AmneziaWG parameters — such a config
// is understood by AmneziaVPN clients, not by the official WireGuard app
// (see docs/awg.md).
func (m *Manager) ClientConfig(d store.Device) string {
	return m.ClientConfigAt(d, node.Endpoint{
		Host:           m.cfg.WGHost,
		WGPort:         m.cfg.WGPort,
		WGServerPubKey: m.PublicKey(),
	})
}

// ClientConfigAt renders the config for an arbitrary node endpoint — with
// multi-node, the active node's host/port/server key go into the [Peer]
// section instead of this box's own (see internal/node). Addressing, DNS and
// MTU are properties of the tunnel and stay the manager's own.
func (m *Manager) ClientConfigAt(d store.Device, ep node.Endpoint) string {
	return m.clientConfig(d, ep, "0.0.0.0/0, ::/0")
}

// ClientConfigGameAt renders the game-tunnel config (docs/dns-games.md §2):
// the same peer and key, but AllowedIPs limited to the selected games' CIDR
// ranges — everything else keeps flowing outside the tunnel, which is what
// keeps the in-game route stable. The client imports it as a SECOND tunnel
// next to the main one.
func (m *Manager) ClientConfigGameAt(d store.Device, ep node.Endpoint, cidrs []string) string {
	allowed := strings.Join(cidrs, ", ")
	if allowed == "" {
		allowed = "0.0.0.0/0, ::/0"
	}
	return m.clientConfig(d, ep, allowed)
}

func (m *Manager) clientConfig(d store.Device, ep node.Endpoint, allowedIPs string) string {
	awgBlock := "\n"
	if m.awg != nil {
		awgBlock = m.awg.ClientINI() + "\n"
	}
	address := fmt.Sprintf("%s/32", d.IPv4)
	if ip6 := m.cfg.IPv6For(d.IPv4); ip6 != "" {
		address += ", " + ip6 + "/128"
	}
	return fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s
DNS = %s
MTU = %d
%s[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = %s
Endpoint = %s:%d
PersistentKeepalive = 25
`, d.PrivateKey, address, m.DNSLine(d), m.cfg.MTU, awgBlock,
		ep.WGServerPubKey, d.PresharedKey, allowedIPs, ep.Host, ep.WGPort)
}

func detectDefaultIface() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return strings.TrimSuffix(fields[i+1], "\n")
		}
	}
	return ""
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func parseInt(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int64(c-'0')
	}
	return n
}
