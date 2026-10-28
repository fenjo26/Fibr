package xray

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/node"
	"github.com/fenjo26/Fibr/internal/store"
)

func newTestManager(t *testing.T, enabled bool) (*Manager, *store.Store, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{
		DataDir:     dir,
		WGHost:      "vpn.example.com",
		Subnet:      "10.8.0.0/24",
		XrayEnabled: enabled,
		XrayPort:    8443,
		XraySNI:     "www.microsoft.com",
		XrayDest:    "www.microsoft.com:443",
		XrayShortID: "0123abcd",
	}
	m, err := New(cfg, st, slog.Default())
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	return m, st, cfg
}

func TestKeypairPersistedAndValid(t *testing.T) {
	m, st, cfg := newTestManager(t, true)
	first := m.PublicKey()
	// Public key must be 43-char base64url of a 32-byte x25519 key.
	raw, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil || len(raw) != 32 {
		t.Fatalf("public key is not base64url(32 bytes): %q err=%v", first, err)
	}
	// A second manager over the same store loads the same key.
	cfg2 := &config.Config{DataDir: cfg.DataDir}
	m2, err := New(cfg2, st, slog.Default())
	if err != nil {
		t.Fatalf("manager 2: %v", err)
	}
	if m2.PublicKey() != first {
		t.Fatalf("reality key not persisted: %q vs %q", m2.PublicKey(), first)
	}
}

func TestNewUUID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewUUID()
		if !re.MatchString(id) {
			t.Fatalf("uuid %q does not look like a v4 uuid", id)
		}
		if seen[id] {
			t.Fatalf("duplicate uuid %q", id)
		}
		seen[id] = true
	}
}

func TestClientLink(t *testing.T) {
	m, _, _ := newTestManager(t, true)
	d := store.Device{Name: "pixel 7", Protocol: store.ProtocolXray, PublicKey: "11111111-2222-4333-8444-555555555555"}
	link := m.ClientLink(d)
	want := "vless://11111111-2222-4333-8444-555555555555@vpn.example.com:8443" +
		"?type=tcp&security=reality&fp=chrome&sni=www.microsoft.com" +
		"&pbk=" + m.PublicKey() + "&sid=0123abcd&flow=xtls-rprx-vision&encryption=none" +
		"#Fibr-pixel%207"
	if link != want {
		t.Fatalf("link mismatch:\n got %q\nwant %q", link, want)
	}

	// The fragment must survive a round-trip and keep spaces out of the URL.
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Fragment != "Fibr-pixel 7" {
		t.Fatalf("fragment mismatch: %q", u.Fragment)
	}
	if u.User.Username() != "11111111-2222-4333-8444-555555555555" {
		t.Fatalf("uuid missing from userinfo: %q", u.User.String())
	}
	q := u.Query()
	if q.Get("security") != "reality" || q.Get("flow") != "xtls-rprx-vision" || q.Get("encryption") != "none" {
		t.Fatalf("query params mismatch: %s", u.RawQuery)
	}
}

func TestBuildServerConfig(t *testing.T) {
	m, _, _ := newTestManager(t, true)
	data, err := m.BuildServerConfig([]string{"uuid-a", "uuid-b"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// Must be valid JSON.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, data)
	}

	var cfg struct {
		Inbounds []struct {
			Listen   string `json:"listen"`
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []struct {
					ID   string `json:"id"`
					Flow string `json:"flow"`
				} `json:"clients"`
				Decryption string `json:"decryption"`
			} `json:"settings"`
			StreamSettings struct {
				Network         string `json:"network"`
				Security        string `json:"security"`
				RealitySettings struct {
					Dest        string   `json:"dest"`
					ServerNames []string `json:"serverNames"`
					PrivateKey  string   `json:"privateKey"`
					ShortIds    []string `json:"shortIds"`
				} `json:"realitySettings"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
		Outbounds []struct {
			Protocol string `json:"protocol"`
			Tag      string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if len(cfg.Inbounds) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(cfg.Inbounds))
	}
	in := cfg.Inbounds[0]
	if in.Protocol != "vless" || in.Port != 8443 || in.Listen != "0.0.0.0" {
		t.Fatalf("inbound header mismatch: %+v", in)
	}
	if in.Settings.Decryption != "none" {
		t.Fatalf("vless decryption must be \"none\", got %q", in.Settings.Decryption)
	}
	if len(in.Settings.Clients) != 2 ||
		in.Settings.Clients[0].ID != "uuid-a" || in.Settings.Clients[1].ID != "uuid-b" {
		t.Fatalf("clients mismatch: %+v", in.Settings.Clients)
	}
	for _, c := range in.Settings.Clients {
		if c.Flow != "xtls-rprx-vision" {
			t.Fatalf("client flow mismatch: %+v", c)
		}
	}
	ss := in.StreamSettings
	if ss.Network != "tcp" || ss.Security != "reality" {
		t.Fatalf("stream settings mismatch: %+v", ss)
	}
	rs := ss.RealitySettings
	if rs.Dest != "www.microsoft.com:443" {
		t.Fatalf("dest mismatch: %q", rs.Dest)
	}
	if len(rs.ServerNames) != 1 || rs.ServerNames[0] != "www.microsoft.com" {
		t.Fatalf("serverNames mismatch: %+v", rs.ServerNames)
	}
	if len(rs.ShortIds) != 1 || rs.ShortIds[0] != "0123abcd" {
		t.Fatalf("shortIds mismatch: %+v", rs.ShortIds)
	}
	// The private key must be present (xray needs it server-side) and must
	// never equal the public key that goes into client links.
	if rs.PrivateKey == "" {
		t.Fatal("reality private key missing from config")
	}
	if rs.PrivateKey == m.PublicKey() {
		t.Fatal("config private key equals the public key")
	}
	tags := map[string]bool{}
	for _, o := range cfg.Outbounds {
		tags[o.Tag] = true
	}
	if !tags["direct"] || !tags["block"] {
		t.Fatalf("outbounds mismatch: %+v", cfg.Outbounds)
	}
}

func TestSyncClients(t *testing.T) {
	m, st, cfg := newTestManager(t, true)

	u := &store.User{Username: "alice"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("user: %v", err)
	}
	mk := func(name string, enabled bool) *store.Device {
		ip, err := st.NextIPv4(cfg.Subnet)
		if err != nil {
			t.Fatalf("ip: %v", err)
		}
		d := &store.Device{
			UserID: u.ID, Name: name, Protocol: store.ProtocolXray,
			PublicKey: NewUUID(), IPv4: ip, Enabled: enabled,
		}
		if err := st.CreateDevice(d); err != nil {
			t.Fatalf("device: %v", err)
		}
		return d
	}
	mk("phone", true)
	mk("laptop", true)
	mk("disabled", false)

	// A wg device must not leak into the xray client list.
	wgIP, _ := st.NextIPv4(cfg.Subnet)
	if err := st.CreateDevice(&store.Device{UserID: u.ID, Name: "pc", Protocol: store.ProtocolWG, PublicKey: "wgpub", IPv4: wgIP, Enabled: true}); err != nil {
		t.Fatalf("wg device: %v", err)
	}

	if err := m.SyncClients(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	data, err := os.ReadFile(m.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if got := strings.Count(string(data), `"flow": "xtls-rprx-vision"`); got != 2 {
		t.Fatalf("expected 2 enabled xray clients in config, found %d:\n%s", got, data)
	}
	if strings.Contains(string(data), "wgpub") {
		t.Fatal("wg device leaked into xray config")
	}

	// Disabled xray: SyncClients is a no-op and writes nothing.
	m2, _, _ := newTestManager(t, false)
	if err := m2.SyncClients(); err != nil {
		t.Fatalf("sync (disabled): %v", err)
	}
	if _, err := os.Stat(m2.ConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("expected no config file when xray disabled, stat err=%v", err)
	}
}

// TestClientLinkAt: rendering against an arbitrary node endpoint replaces
// the vless host/port while sni/pbk/sid come from the endpoint too; the
// plain ClientLink keeps the local values.
func TestClientLinkAt(t *testing.T) {
	m, _, _ := newTestManager(t, true)
	d := store.Device{Name: "pixel", Protocol: store.ProtocolXray, PublicKey: "11111111-2222-4333-8444-555555555555"}
	ep := node.Endpoint{
		Host:          "edge1.example.com",
		XrayPort:      9443,
		XrayPubKeyB64: "REMOTEPBKAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		XrayShortID:   "ffff0000",
		XraySNI:       "www.apple.com",
	}
	link := m.ClientLinkAt(d, ep)
	want := "vless://11111111-2222-4333-8444-555555555555@edge1.example.com:9443" +
		"?type=tcp&security=reality&fp=chrome&sni=www.apple.com" +
		"&pbk=" + ep.XrayPubKeyB64 + "&sid=ffff0000&flow=xtls-rprx-vision&encryption=none" +
		"#Fibr-pixel"
	if link != want {
		t.Fatalf("link mismatch:\n got %q\nwant %q", link, want)
	}
	// The manager's own key must not leak into the remote rendering.
	if strings.Contains(link, m.PublicKey()) {
		t.Fatalf("local reality key leaked into remote endpoint link: %s", link)
	}
	// Local rendering unchanged.
	if got := m.ClientLink(d); !strings.Contains(got, "@vpn.example.com:8443") || !strings.Contains(got, m.PublicKey()) {
		t.Fatalf("local rendering broken: %s", got)
	}
}

// TestSyncClientsDropsExpired: SyncClients uses ListActiveDevices, so an
// xray device of an expired user (own deadline or the owner cascade) must
// not appear in the rendered inbound — the billing fix mirroring wg peers.
func TestSyncClientsDropsExpired(t *testing.T) {
	m, st, cfg := newTestManager(t, true)

	mkUser := func(name string) *store.User {
		u := &store.User{Username: name}
		if err := st.CreateUser(u); err != nil {
			t.Fatalf("seed user %s: %v", name, err)
		}
		return u
	}
	paid := mkUser("paid")
	expired := mkUser("expired")
	past := time.Now().Add(-time.Hour).Unix()
	if err := st.SetUserExpiry(expired.ID, &past); err != nil {
		t.Fatalf("expire: %v", err)
	}

	mkXray := func(uid int64, name string) string {
		ip, _ := st.NextIPv4(cfg.Subnet)
		d := &store.Device{UserID: uid, Name: name, Protocol: store.ProtocolXray, PublicKey: NewUUID(), IPv4: ip, Enabled: true}
		if err := st.CreateDevice(d); err != nil {
			t.Fatalf("seed device %s: %v", name, err)
		}
		return d.PublicKey
	}
	paidUUID := mkXray(paid.ID, "paid-phone")
	expiredUUID := mkXray(expired.ID, "expired-phone")

	if err := m.SyncClients(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	data, err := os.ReadFile(m.ConfigPath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), paidUUID) {
		t.Fatalf("paid uuid missing from xray config:\n%s", data)
	}
	if strings.Contains(string(data), expiredUUID) {
		t.Fatalf("expired uuid must be dropped from xray config:\n%s", data)
	}
}

// TestGenerateRealityKeypair: the agent-side key generation helper produces
// decodable base64url x25519 material.
func TestGenerateRealityKeypair(t *testing.T) {
	priv, pub, err := GenerateRealityKeypair()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	rawPriv, err1 := base64.RawURLEncoding.DecodeString(priv)
	rawPub, err2 := base64.RawURLEncoding.DecodeString(pub)
	if err1 != nil || err2 != nil || len(rawPriv) != 32 || len(rawPub) != 32 {
		t.Fatalf("keypair not base64url(32): %q %q %v %v", priv, pub, err1, err2)
	}
	if priv == pub {
		t.Fatal("private and public halves must differ")
	}
}
