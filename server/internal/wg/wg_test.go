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
		DNS:         []string{"1.1.1.1", "1.0.0.1"},
		MTU:         1420,
	}
	m, err := New(cfg, st, slog.Default())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	return m, st
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
		"Address = 10.8.0.2/32",
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
