package main

// Agent tests: pure diff helpers, key persistence, and one full passive
// one-tick integration against a real api.Handler control plane (httptest).

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fenjo26/Fibr/internal/api"
	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/node"
	"github.com/fenjo26/Fibr/internal/store"
	"github.com/fenjo26/Fibr/internal/wg"
	"github.com/fenjo26/Fibr/internal/xray"
)

func TestPeersEqual(t *testing.T) {
	p := func(id int64, pub, psk string, ips ...string) node.Peer {
		return node.Peer{DeviceID: id, PublicKey: pub, PresharedKey: psk, AllowedIPs: ips}
	}
	a := []node.Peer{p(1, "pub1", "psk1", "10.8.0.2/32", "fd42::2/128")}
	sameReordered := []node.Peer{p(1, "pub1", "psk1", "10.8.0.2/32", "fd42::2/128")}
	if !peersEqual(a, sameReordered) {
		t.Fatal("identical sets must be equal")
	}
	cases := [][]node.Peer{
		{p(1, "pub1", "psk1")},                // different length
		{p(2, "pub1", "psk1")},                // different device id
		{p(1, "pub2", "psk1")},                // different key
		{p(1, "pub1", "psk2")},                // different psk
		{p(1, "pub1", "psk1", "10.8.0.3/32")}, // different allowed ips
		{p(1, "pub1", "psk1", "fd42::2/128", "10.8.0.2/32"), p(1, "pub1", "psk1", "10.8.0.2/32", "fd42::2/128")}, // dup vs single
	}
	for i, b := range cases {
		if peersEqual(a, b) {
			t.Errorf("case %d: sets must differ: %v vs %v", i, a, b)
		}
	}
	if !peersEqual(nil, []node.Peer{}) {
		t.Fatal("nil and empty must both mean 'no peers'")
	}
}

func TestLoadKeysPersists(t *testing.T) {
	dir := t.TempDir()

	priv1, xpriv1, xpub1, xsid1, err := LoadKeys(dir, true)
	if err != nil {
		t.Fatalf("first load (xray on): %v", err)
	}
	if xpriv1 == "" || xpub1 == "" || len(xsid1) != 8 {
		t.Fatalf("reality material incomplete: %q %q %q", xpriv1, xpub1, xsid1)
	}
	// Node key file is private.
	fi, err := os.Stat(filepath.Join(dir, "node.key"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("node.key must exist 0600: %v %+v", err, fi)
	}

	priv2, xpriv2, xpub2, xsid2, err := LoadKeys(dir, true)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if priv1.String() != priv2.String() || xpriv1 != xpriv2 || xpub1 != xpub2 || xsid1 != xsid2 {
		t.Fatal("keys must be load-once stable (the control plane pins them)")
	}

	// Xray disabled: no reality files needed or read.
	_, xpriv3, _, _, err := LoadKeys(dir, false)
	if err != nil || xpriv3 != "" {
		t.Fatalf("xray-off load: %v %q", err, xpriv3)
	}
}

// fakeLayer records what the agent would have done to the interface.
type fakeLayer struct {
	applied     [][]node.Peer // one entry per Apply call
	ensureCalls int
	lastV4      string
	stats       []node.PeerStats // returned by Stats
}

func (f *fakeLayer) EnsureInterface(v4, v6 string) error {
	f.ensureCalls++
	f.lastV4 = v4
	return nil
}

func (f *fakeLayer) Apply(peers []node.Peer) error {
	f.applied = append(f.applied, peers)
	return nil
}

func (f *fakeLayer) Stats(_ []node.Peer) []node.PeerStats { return f.stats }

// newControlPlane boots a real API over temp storage and serves it.
func newControlPlane(t *testing.T) (*httptest.Server, *store.Store, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "fibr.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{
		Domain: "vpn.example.com", WGHost: "vpn.example.com", WGPort: 51820,
		WGInterface: "wg0", Subnet: "10.8.0.0/24", ServerIP: "10.8.0.1",
		Subnet6: "fd42:f1b2::/64", ServerIP6: "fd42:f1b2::1",
		DNS: []string{"1.1.1.1"}, MTU: 1420, MaxDevices: 10,
		AdminToken: "test-admin-token", Secret: "test-secret",
		XrayEnabled: true, XrayPort: 8443,
		XraySNI: "www.microsoft.com", XrayDest: "www.microsoft.com:443", XrayShortID: "0123abcd",
		DataDir: dir,
	}
	wgm, err := wg.New(cfg, st, slog.Default())
	if err != nil {
		t.Fatalf("wg manager: %v", err)
	}
	xm, err := xray.New(cfg, st, slog.Default())
	if err != nil {
		t.Fatalf("xray manager: %v", err)
	}
	a := api.New(cfg, st, wgm, xm, slog.Default())
	if _, err := store.EnsureLocalNode(st, cfg.WGHost, cfg.WGPort, &cfg.XrayPort); err != nil {
		t.Fatalf("ensure local node: %v", err)
	}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	return srv, st, cfg
}

// TestAgentOneTickPassive walks the full agent flow in passive mode against
// a live control plane: register, first sync applies peers, a stats-bearing
// sync folds into the store, the xray config is rendered, and blocking the
// node clears the peers.
func TestAgentOneTickPassive(t *testing.T) {
	srv, st, _ := newControlPlane(t)
	ctx := context.Background()

	// One live wg device (valid keys — the agent parses what it receives).
	u := &store.User{Username: "alice"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	d := &store.Device{UserID: u.ID, Name: "phone", Protocol: store.ProtocolWG,
		PublicKey:  "PUBKEYPHONEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		PrivateKey: "priv", PresharedKey: "PSKPHONEAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		IPv4: "10.8.0.2", Enabled: true}
	if err := st.CreateDevice(d); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	xu := &store.User{Username: "bob"}
	if err := st.CreateUser(xu); err != nil {
		t.Fatalf("seed xray user: %v", err)
	}
	xd := &store.Device{UserID: xu.ID, Name: "pixel", Protocol: store.ProtocolXray,
		PublicKey: "uuid-42", IPv4: "10.8.0.3", Enabled: true}
	if err := st.CreateDevice(xd); err != nil {
		t.Fatalf("seed xray device: %v", err)
	}

	// The node the agent will enrol as.
	edge := &store.Node{Name: "edge", Host: "edge.example.com", WGPort: 51820, Token: "agent-test-token-0000000000000000"}
	if err := st.CreateNode(edge); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	dataDir := t.TempDir()
	ag, err := NewAgent(Config{
		APIURL: srv.URL, Token: edge.Token, DataDir: dataDir,
		Passive: true, XrayEnabled: true, Interval: time.Second,
	}, slog.Default())
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	fake := &fakeLayer{}
	ag.wg = fake

	// Registration completes the node server-side.
	if err := ag.Register(ctx); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, _ := st.GetNode(edge.ID)
	if got.Status != store.NodeReady || got.ServerPublicKey == nil || *got.ServerPublicKey != ag.priv.PublicKey().String() {
		t.Fatalf("node not registered: %+v", got)
	}

	// First tick applies the wg peer and writes the xray config.
	if err := ag.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(fake.applied) != 1 || len(fake.applied[0]) != 1 {
		t.Fatalf("first tick must apply exactly the wg peer, got %+v", fake.applied)
	}
	if peer := fake.applied[0][0]; peer.DeviceID != d.ID || peer.PublicKey != d.PublicKey {
		t.Fatalf("applied peer mismatch: %+v", peer)
	}
	if fake.lastV4 != "10.8.0.1" {
		t.Fatalf("interface address must come from the sync response, got %q", fake.lastV4)
	}
	xrayCfg, err := os.ReadFile(filepath.Join(dataDir, "xray", "config.json"))
	if err != nil {
		t.Fatalf("xray config not written: %v", err)
	}
	if !strings.Contains(string(xrayCfg), "uuid-42") || !strings.Contains(string(xrayCfg), ag.xraySID) {
		t.Fatalf("xray config must carry the uuid and the node's own short id:\n%s", xrayCfg)
	}
	if strings.Contains(string(xrayCfg), "0123abcd") {
		t.Fatalf("control-plane short id must not leak into the node config:\n%s", xrayCfg)
	}

	// Second tick with stats: they must land on the device in the store.
	hs := time.Now().Unix()
	fake.stats = []node.PeerStats{{DeviceID: d.ID, LastHandshake: hs, Rx: 1000, Tx: 2000}}
	if err := ag.Tick(ctx); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	dev, _ := st.GetDevice(d.ID)
	if dev.LastHandshake.Unix() != hs || dev.TotalRx != 1000 || dev.TotalTx != 2000 {
		t.Fatalf("stats not folded into the control plane: %+v", dev)
	}
	// Same peer set, same revision: no re-apply.
	if len(fake.applied) != 1 {
		t.Fatalf("unchanged state must not re-apply peers: %+v", fake.applied)
	}

	// A control-plane mutation bumps the revision; the next tick re-applies.
	if _, err := st.BumpPeerRevision(); err != nil {
		t.Fatalf("bump: %v", err)
	}
	if err := ag.Tick(ctx); err != nil {
		t.Fatalf("tick 3: %v", err)
	}
	if len(fake.applied) != 2 {
		t.Fatalf("revision advance must re-apply peers: %+v", fake.applied)
	}

	// Blocking the node clears the peers on the next tick.
	if err := st.SetNodeStatus(edge.ID, store.NodeBlocked); err != nil {
		t.Fatalf("block: %v", err)
	}
	if err := ag.Tick(ctx); err != nil {
		t.Fatalf("tick 4: %v", err)
	}
	if len(fake.applied) != 3 || len(fake.applied[2]) != 0 {
		t.Fatalf("blocked node must apply an empty peer set: %+v", fake.applied)
	}
	// The agent persisted the revision it last saw.
	raw, err := os.ReadFile(filepath.Join(dataDir, "revision"))
	if err != nil || strings.TrimSpace(string(raw)) == "0" {
		t.Fatalf("revision not persisted: %v %q", err, raw)
	}
}

// TestAgentRegisterConflictIsFatal: a re-keyed control plane (node deleted
// and re-created) rejects the old identity with 409 and registration fails.
func TestAgentRegisterConflictIsFatal(t *testing.T) {
	srv, st, _ := newControlPlane(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	edge := &store.Node{Name: "edge", Host: "edge.example.com", WGPort: 51820, Token: "tok-old-identity"}
	if err := st.CreateNode(edge); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	dataDir := t.TempDir()
	ag, err := NewAgent(Config{APIURL: srv.URL, Token: edge.Token, DataDir: dataDir, Passive: true, Interval: time.Second}, slog.Default())
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := ag.Register(ctx); err != nil {
		t.Fatalf("first register: %v", err)
	}

	// Control plane side: block, then re-register with a DIFFERENT key by
	// wiping the node and creating a fresh one reusing the token value is
	// impossible (tokens are unique) — simulate the mismatch by registering
	// a foreign key first through the API path.
	foreign, err := NewAgent(Config{APIURL: srv.URL, Token: edge.Token, DataDir: t.TempDir(), Passive: true, Interval: time.Second}, slog.Default())
	if err != nil {
		t.Fatalf("foreign agent: %v", err)
	}
	err = foreign.Register(ctx)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("foreign key must fail with a mismatch error, got %v", err)
	}
}

// TestSyncResponseShape pins the shared wire JSON so a control-plane rename
// cannot silently break the agent parser.
func TestSyncResponseShape(t *testing.T) {
	raw := `{"status":"ready","revision":7,"peers":[{"device_id":3,"public_key":"k","psk":"p","allowed_ips":["10.8.0.2/32"]}],"xray_uuids":["u"],"server_ip_v4":"10.8.0.1","server_ip_v6":"fd42::1","xray_sni":"s","xray_dest":"d"}`
	var resp node.SyncResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ready" || resp.Revision != 7 || len(resp.Peers) != 1 ||
		resp.Peers[0].DeviceID != 3 || resp.Peers[0].PresharedKey != "p" ||
		resp.ServerIPv4 != "10.8.0.1" || resp.XraySNI != "s" {
		t.Fatalf("wire shape drifted: %+v", resp)
	}
}
