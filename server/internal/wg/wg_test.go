package wg

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/store"
)

func newTestManager(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{
		Domain:      "vpn.example.com",
		WGHost:      "vpn.example.com",
		WGPort:      51820,
		WGInterface: "wg0",
		Subnet:      "10.8.0.0/24",
		ServerIP:    "10.8.0.1",
		Subnet6:     "fd42:f1b2::/64",
		ServerIP6:   "fd42:f1b2::1",
		DNS:         []string{"1.1.1.1", "1.0.0.1"},
		MTU:         1420,
	}
	return newTestManagerCfg(t, cfg, st), st
}

func newTestManagerCfg(t *testing.T, cfg *config.Config, st *store.Store) *Manager {
	t.Helper()
	m, err := New(cfg, st, slog.Default())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	return m
}

func TestServerKeyPersisted(t *testing.T) {
	m, st := newTestManager(t)
	first := m.PublicKey()
	if len(first) != 44 { // base64-encoded 32-byte Curve25519 key
		t.Fatalf("public key does not look like a 44-char base64 key: %q", first)
	}
	// A second manager over the same store must load the same key.
	cfg := &config.Config{Subnet: "10.8.0.0/24", ServerIP: "10.8.0.1"}
	m2, err := New(cfg, st, slog.Default())
	if err != nil {
		t.Fatalf("manager 2: %v", err)
	}
	if m2.PublicKey() != first {
		t.Fatalf("server key not persisted: %q vs %q", m2.PublicKey(), first)
	}
}

func TestClientConfig(t *testing.T) {
	m, st := newTestManager(t)
	u := &store.User{Username: "bob"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("user: %v", err)
	}
	ip, err := st.NextIPv4("10.8.0.0/24")
	if err != nil {
		t.Fatalf("ip: %v", err)
	}
	d := store.Device{UserID: u.ID, Name: "phone", PublicKey: "pub", PrivateKey: "PRIVKEY", PresharedKey: "PSK", IPv4: ip, Enabled: true}
	conf := m.ClientConfig(d)

	for _, want := range []string{
		"PrivateKey = PRIVKEY",
		"Address = 10.8.0.2/32, fd42:f1b2::2/128",
		"DNS = 1.1.1.1, 1.0.0.1",
		"MTU = 1420",
		"PublicKey = " + m.PublicKey(),
		"PresharedKey = PSK",
		"AllowedIPs = 0.0.0.0/0, ::/0",
		"Endpoint = vpn.example.com:51820",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("config missing %q:\n%s", want, conf)
		}
	}
}

func TestPassiveMode(t *testing.T) {
	m, _ := newTestManager(t)
	if !m.Passive() {
		t.Skip("kernel WireGuard control available; passive-mode test not applicable")
	}
	// EnsureInterface and SyncPeers must be safe no-ops in passive mode.
	if err := m.EnsureInterface(); err != nil {
		t.Fatalf("EnsureInterface in passive mode: %v", err)
	}
	if err := m.SyncPeers(); err != nil {
		t.Fatalf("SyncPeers in passive mode: %v", err)
	}
	m.CollectStats() // must not panic
	m.Throughput()   // must not panic
}

func testDevice(t *testing.T, st *store.Store) store.Device {
	t.Helper()
	u := &store.User{Username: "bob"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("user: %v", err)
	}
	ip, err := st.NextIPv4("10.8.0.0/24")
	if err != nil {
		t.Fatalf("ip: %v", err)
	}
	return store.Device{UserID: u.ID, Name: "phone", PublicKey: "pub", PrivateKey: "PRIVKEY", PresharedKey: "PSK", IPv4: ip, Enabled: true}
}

func TestClientConfigAWGEngine(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	cfg := &config.Config{
		Domain:   "vpn.example.com",
		WGHost:   "vpn.example.com",
		WGPort:   51820,
		WGEngine: config.EngineAWG,
		Subnet:   "10.8.0.0/24",
		ServerIP: "10.8.0.1",
		DNS:      []string{"1.1.1.1"},
		MTU:      1420,
	}
	m := newTestManagerCfg(t, cfg, st)
	if m.awg == nil {
		t.Fatal("awg engine must load params into the manager")
	}
	conf := m.ClientConfig(testDevice(t, st))

	// AWG block sits in [Interface], before [Peer], with all nine fields.
	peerAt := strings.Index(conf, "[Peer]")
	for _, want := range []string{
		"Jc = ", "Jmin = ", "Jmax = ", "S1 = ", "S2 = ",
		"H1 = ", "H2 = ", "H3 = ", "H4 = ",
	} {
		if !strings.Contains(conf[:peerAt], want) {
			t.Errorf("awg config missing %q before [Peer]:\n%s", want, conf)
		}
	}
	// Both managers over the same store must render identical AWG blocks.
	m2 := newTestManagerCfg(t, cfg, st)
	if m.awg.ClientINI() != m2.awg.ClientINI() {
		t.Fatal("awg params must be persisted and stable across restarts")
	}
}

func TestClientConfigKernelEngineHasNoAWG(t *testing.T) {
	m, st := newTestManager(t)
	conf := m.ClientConfig(testDevice(t, st))
	for _, banned := range []string{"Jc =", "Jmin =", "S1 =", "H1 ="} {
		if strings.Contains(conf, banned) {
			t.Errorf("kernel engine config must not contain awg field %q:\n%s", banned, conf)
		}
	}
}
