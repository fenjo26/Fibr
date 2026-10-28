package api

// Multi-node endpoints tests (see docs/multinode.md): the superadmin-only
// admin surface, the agent register/sync surface, and endpoint-aware config
// rendering.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fenjo26/Fibr/internal/node"
	"github.com/fenjo26/Fibr/internal/store"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// ensureLocalNode mirrors what cmd/fibr does on boot, so the API under test
// behaves like a real control plane.
func ensureLocalNode(t *testing.T, a *API, st *store.Store) int64 {
	t.Helper()
	var xp *int
	if a.cfg.XrayEnabled {
		p := a.cfg.XrayPort
		xp = &p
	}
	id, err := store.EnsureLocalNode(st, a.cfg.WGHost, a.cfg.WGPort, xp)
	if err != nil {
		t.Fatalf("ensure local node: %v", err)
	}
	return id
}

// nodeReq fires an agent-API request with the given node token.
func nodeReq(a *API, token, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

// createNodeViaAPI creates a pending node through the admin API and returns
// (nodeID, token).
func createNodeViaAPI(t *testing.T, a *API, name, host string) (int64, string) {
	t.Helper()
	rec := adminReq(a, http.MethodPost, "/api/admin/nodes",
		`{"name":"`+name+`","host":"`+host+`","wg_port":51821,"xray_port":9443}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create node: status %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Node struct {
			ID int64 `json:"id"`
		} `json:"node"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("bad create response: %v %s", err, rec.Body.String())
	}
	return out.Node.ID, out.Token
}

// registerNode walks the agent registration call with a real wg keypair.
func registerNode(t *testing.T, a *API, token string, key wgtypes.Key) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"server_public_key":"` + key.PublicKey().String() + `"}`
	return nodeReq(a, token, http.MethodPost, "/api/node/register", body)
}

// nodeFields is the subset of nodeOut the tests assert on.
type nodeFields struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Status   string `json:"status"`
	Token    string `json:"token"`
	IsActive bool   `json:"is_active"`
	Online   bool   `json:"online"`
	Peers    int    `json:"peers"`
}

func listNodes(t *testing.T, a *API) (map[int64]nodeFields, int64) {
	t.Helper()
	rec := adminReq(a, http.MethodGet, "/api/admin/nodes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list nodes: status %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Nodes        []nodeFields `json:"nodes"`
		ActiveNodeID int64        `json:"active_node_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad list json: %v %s", err, rec.Body.String())
	}
	m := make(map[int64]nodeFields, len(out.Nodes))
	for _, n := range out.Nodes {
		m[n.ID] = n
	}
	return m, out.ActiveNodeID
}

// TestNodeAdminSurfaceIsSuperadminOnly: every admin node endpoint 403s an
// admin session and answers the static token.
func TestNodeAdminSurfaceIsSuperadminOnly(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)
	localID := ensureLocalNode(t, a, st)
	edgeID, _ := createNodeViaAPI(t, a, "edge", "edge.example.com")

	admin := func(method, path, body string) int {
		rec := sessReq(a, h.resTok, method, path, body)
		return rec.Code
	}
	paths := []struct{ method, path string }{
		{http.MethodGet, "/api/admin/nodes"},
		{http.MethodPost, "/api/admin/nodes"},
		{http.MethodPatch, "/api/admin/nodes/" + itoa(edgeID)},
		{http.MethodDelete, "/api/admin/nodes/" + itoa(edgeID)},
		{http.MethodPost, "/api/admin/nodes/" + itoa(edgeID) + "/activate"},
		{http.MethodPost, "/api/admin/nodes/" + itoa(edgeID) + "/token"},
	}
	for _, p := range paths {
		if c := admin(p.method, p.path, `{}`); c != http.StatusForbidden {
			t.Fatalf("admin on %s %s: expected 403, got %d", p.method, p.path, c)
		}
	}
	// The superadmin gets 200s everywhere (404 for a missing id).
	if c := adminReq(a, http.MethodGet, "/api/admin/nodes", "").Code; c != http.StatusOK {
		t.Fatalf("superadmin list: %d", c)
	}
	if c := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(edgeID), `{}`).Code; c != http.StatusOK {
		t.Fatalf("superadmin patch: %d", c)
	}
	if c := adminReq(a, http.MethodPost, "/api/admin/nodes/"+itoa(edgeID)+"/token", "").Code; c != http.StatusOK {
		t.Fatalf("superadmin rotate: %d", c)
	}
	if c := adminReq(a, http.MethodDelete, "/api/admin/nodes/9999", "").Code; c != http.StatusNotFound {
		t.Fatalf("missing node delete: %d", c)
	}
	_ = localID
}

// TestNodeCreateListTokenHandling: create returns the token once, the list
// never carries it, duplicate names are 400.
func TestNodeCreateListTokenHandling(t *testing.T) {
	a, st := newTestAPI(t, false)
	seedHierarchy(t, a, st)
	localID := ensureLocalNode(t, a, st)
	edgeID, token := createNodeViaAPI(t, a, "edge", "edge.example.com")
	if len(token) < 32 {
		t.Fatalf("agent token must be a long random string, got %q", token)
	}

	nodes, activeID := listNodes(t, a)
	if activeID != localID {
		t.Fatalf("local must be the default active node, got %d", activeID)
	}
	local := nodes[localID]
	if !local.IsActive || !local.Online || local.Status != store.NodeReady {
		t.Fatalf("local node flags: %+v", local)
	}
	edge := nodes[edgeID]
	if edge.IsActive || edge.Status != store.NodePending || edge.Online {
		t.Fatalf("fresh node flags: %+v", edge)
	}
	for _, n := range nodes {
		if n.Token != "" {
			t.Fatalf("node list must never include the token, got %q for %s", n.Token, n.Name)
		}
	}

	// Duplicate name and missing fields are 400s.
	if rec := adminReq(a, http.MethodPost, "/api/admin/nodes", `{"name":"edge","host":"x.example.com"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate name: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminReq(a, http.MethodPost, "/api/admin/nodes", `{"name":"" ,"host":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty fields: %d", rec.Code)
	}
}

// TestNodeActivateSwitchesEndpoint: after registering and activating a
// remote node, rendered configs carry ITS host/port/pubkey, and
// endpoint_changed_at bumps. Fetching a config stamps config_fetched_at.
func TestNodeActivateSwitchesEndpoint(t *testing.T) {
	a, st := newTestAPI(t, true)
	h := seedHierarchy(t, a, st) // ownUser + ownDev (a wg device)
	localID := ensureLocalNode(t, a, st)
	edgeID, token := createNodeViaAPI(t, a, "edge", "edge.example.com")

	// Register with a real keypair (no reality fields: xray on the node is
	// optional at registration time).
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	if rec := registerNode(t, a, token, key); rec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var reg node.RegisterResponse
	if err := json.Unmarshal(registerNode(t, a, token, key).Body.Bytes(), &reg); err != nil || reg.NodeID != edgeID {
		t.Fatalf("register response: %v %+v", err, reg)
	}
	if n, _ := st.GetNode(edgeID); n.Status != store.NodeReady {
		t.Fatalf("node must be ready after register: %+v", n)
	}

	// Before the switch the config renders the local endpoint.
	meBefore := sessReq(a, a.newSessionToken(h.ownUser.ID), http.MethodGet, "/api/me", "")
	var me struct {
		ActiveNode        string `json:"active_node"`
		EndpointChangedAt int64  `json:"endpoint_changed_at"`
	}
	json.Unmarshal(meBefore.Body.Bytes(), &me)
	if me.ActiveNode != "local" || me.EndpointChangedAt != 0 {
		t.Fatalf("initial endpoint state: %+v", me)
	}

	rec := adminReq(a, http.MethodPost, "/api/admin/nodes/"+itoa(edgeID)+"/activate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}

	// The switch is visible in /api/me and the overview.
	meAfter := sessReq(a, a.newSessionToken(h.ownUser.ID), http.MethodGet, "/api/me", "")
	json.Unmarshal(meAfter.Body.Bytes(), &me)
	if me.ActiveNode != "edge" || me.EndpointChangedAt == 0 {
		t.Fatalf("post-activate endpoint state: %+v", me)
	}
	ovRec := adminReq(a, http.MethodGet, "/api/admin/overview", "")
	var ov struct {
		Server struct {
			Host      string `json:"host"`
			Port      int    `json:"port"`
			PublicKey string `json:"public_key"`
		} `json:"server"`
		ActiveNode        string `json:"active_node"`
		EndpointChangedAt int64  `json:"endpoint_changed_at"`
	}
	json.Unmarshal(ovRec.Body.Bytes(), &ov)
	if ov.Server.Host != "edge.example.com" || ov.Server.Port != 51821 || ov.Server.PublicKey != key.PublicKey().String() {
		t.Fatalf("overview endpoint mismatch: %+v", ov.Server)
	}
	if ov.ActiveNode != "edge" || ov.EndpointChangedAt != me.EndpointChangedAt {
		t.Fatalf("overview node state: %+v", ov)
	}

	// The rendered device config carries the remote node's endpoint and key.
	tok := a.newSessionToken(h.ownUser.ID)
	cfgRec := sessReq(a, tok, http.MethodGet, "/api/me/devices/"+itoa(h.ownDev.ID)+"/config", "")
	if cfgRec.Code != http.StatusOK {
		t.Fatalf("config: %d %s", cfgRec.Code, cfgRec.Body.String())
	}
	cfg := cfgRec.Body.String()
	for _, want := range []string{
		"Endpoint = edge.example.com:51821",
		"PublicKey = " + key.PublicKey().String(),
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("switched config missing %q:\n%s", want, cfg)
		}
	}

	// Serving the config stamped config_fetched_at.
	d, _ := st.GetDevice(h.ownDev.ID)
	if d.ConfigFetchedAt == nil {
		t.Fatal("config fetch not stamped")
	}
	_ = localID
}

// TestNodePatchGuards: local host/port edits are refused, blocking or
// deleting the active node is refused, activating a non-ready node is
// refused, host edits on the active node bump endpoint_changed_at.
func TestNodePatchGuards(t *testing.T) {
	a, st := newTestAPI(t, false)
	seedHierarchy(t, a, st)
	localID := ensureLocalNode(t, a, st)
	edgeID, _ := createNodeViaAPI(t, a, "edge", "edge.example.com")

	// Local node: host/port follow env.
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(localID), `{"host":"hacked.example.com"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("local host edit: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(localID), `{"wg_port":1234}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("local port edit: %d %s", rec.Code, rec.Body.String())
	}
	// Renaming the local node is fine, host untouched.
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(localID), `{"name":"hub"}`); rec.Code != http.StatusOK {
		t.Fatalf("local rename: %d %s", rec.Code, rec.Body.String())
	}
	// Local node cannot be deleted.
	if rec := adminReq(a, http.MethodDelete, "/api/admin/nodes/"+itoa(localID), ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("local delete: %d", rec.Code)
	}

	// Cannot activate a pending node.
	if rec := adminReq(a, http.MethodPost, "/api/admin/nodes/"+itoa(edgeID)+"/activate", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("activate pending: %d", rec.Code)
	}

	// Register the edge node, then exercise the guards on the ACTIVE node.
	key, _ := wgtypes.GeneratePrivateKey()
	registerNode(t, a, mustNodeToken(t, st, edgeID), key)

	// Blocking the active node: refused.
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(localID), `{"status":"blocked"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("block active: %d %s", rec.Code, rec.Body.String())
	}
	// Deleting the active node: refused.
	if rec := adminReq(a, http.MethodDelete, "/api/admin/nodes/"+itoa(localID), ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("delete active: %d", rec.Code)
	}
	// Bad status value.
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(edgeID), `{"status":"pending"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status: %d", rec.Code)
	}

	// Host edit on the ACTIVE node bumps endpoint_changed_at.
	before := endpointChangedSetting(t, st)
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(localID), `{"name":"local"}`); rec.Code != http.StatusOK {
		t.Fatalf("no-op patch: %d", rec.Code)
	}
	if endpointChangedSetting(t, st) != before {
		t.Fatal("no-op patch must not bump endpoint_changed_at")
	}
	// Switch to edge, then edit edge's host while it is active: bump. (The
	// setting is rewound to a tiny value first so the test cannot collide
	// with the activate bump inside the same wall-clock second.)
	adminReq(a, http.MethodPost, "/api/admin/nodes/"+itoa(edgeID)+"/activate", "")
	if err := st.SetSettingInt(store.SettingEndpointChangedAt, 1); err != nil {
		t.Fatalf("rewind endpoint_changed_at: %v", err)
	}
	before = endpointChangedSetting(t, st)
	if before != 1 {
		t.Fatalf("rewind failed: %d", before)
	}
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(edgeID), `{"host":"edge2.example.com"}`); rec.Code != http.StatusOK {
		t.Fatalf("edge host edit: %d %s", rec.Code, rec.Body.String())
	}
	after := endpointChangedSetting(t, st)
	if after <= before {
		t.Fatalf("active-node host edit must bump endpoint_changed_at: %d -> %d", before, after)
	}

	// Now the edge (active) can be blocked? No — still active.
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(edgeID), `{"status":"blocked"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("block still-active: %d", rec.Code)
	}
	// Switch back to local, then block edge: fine; re-activating a blocked
	// node is refused.
	adminReq(a, http.MethodPost, "/api/admin/nodes/"+itoa(localID)+"/activate", "")
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(edgeID), `{"status":"blocked"}`); rec.Code != http.StatusOK {
		t.Fatalf("block inactive: %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminReq(a, http.MethodPost, "/api/admin/nodes/"+itoa(edgeID)+"/activate", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("activate blocked: %d", rec.Code)
	}
	// Unblock restores activatability.
	if rec := adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(edgeID), `{"status":"ready"}`); rec.Code != http.StatusOK {
		t.Fatalf("unblock: %d", rec.Code)
	}
	// Deleting an inactive node works.
	if rec := adminReq(a, http.MethodDelete, "/api/admin/nodes/"+itoa(edgeID), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete inactive: %d", rec.Code)
	}
}

func endpointChangedSetting(t *testing.T, st *store.Store) int64 {
	t.Helper()
	v, err := st.GetSettingInt(store.SettingEndpointChangedAt)
	if err != nil {
		t.Fatalf("read endpoint_changed_at: %v", err)
	}
	return v
}

func mustNodeToken(t *testing.T, st *store.Store, id int64) string {
	t.Helper()
	n, err := st.GetNode(id)
	if err != nil {
		t.Fatalf("node %d: %v", id, err)
	}
	return n.Token
}

// TestNodeRegisterAuthAndKeys: wrong token 401, the local placeholder token
// 401, same-key re-register is idempotent, a different key is a hard 409,
// blocked nodes are refused.
func TestNodeRegisterAuthAndKeys(t *testing.T) {
	a, st := newTestAPI(t, false)
	seedHierarchy(t, a, st)
	ensureLocalNode(t, a, st)
	edgeID, token := createNodeViaAPI(t, a, "edge", "edge.example.com")

	if rec := nodeReq(a, "wrong-token", http.MethodPost, "/api/node/register", `{"server_public_key":"k"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	// The local node's placeholder token must never authenticate an agent.
	local, _ := st.GetNodeByName(store.LocalNodeName)
	if rec := nodeReq(a, local.Token, http.MethodPost, "/api/node/register", `{"server_public_key":"k"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("local placeholder token: %d", rec.Code)
	}
	if rec := nodeReq(a, "", http.MethodPost, "/api/node/register", `{"server_public_key":"k"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", rec.Code)
	}

	key, _ := wgtypes.GeneratePrivateKey()
	if rec := registerNode(t, a, token, key); rec.Code != http.StatusOK {
		t.Fatalf("first register: %d %s", rec.Code, rec.Body.String())
	}
	n, _ := st.GetNode(edgeID)
	if n.LastSeenAt == nil {
		t.Fatal("register must stamp last_seen")
	}
	// Same key: idempotent 200.
	if rec := registerNode(t, a, token, key); rec.Code != http.StatusOK {
		t.Fatalf("idempotent register: %d %s", rec.Code, rec.Body.String())
	}
	// Different key: 409 — issued configs embed the old one.
	other, _ := wgtypes.GeneratePrivateKey()
	if rec := registerNode(t, a, token, other); rec.Code != http.StatusConflict {
		t.Fatalf("key mismatch: %d %s", rec.Code, rec.Body.String())
	}
	// Missing key: 400.
	if rec := nodeReq(a, token, http.MethodPost, "/api/node/register", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing key: %d", rec.Code)
	}

	// Blocked nodes cannot re-register.
	adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(edgeID), `{"status":"blocked"}`)
	if rec := registerNode(t, a, token, key); rec.Code != http.StatusForbidden {
		t.Fatalf("blocked register: %d", rec.Code)
	}
}

// TestNodeSync: peers come only from active wg devices (expired users drop
// out), stats are folded into devices, last_seen updates, and a blocked node
// gets an empty peer list with status blocked.
func TestNodeSync(t *testing.T) {
	a, st := newTestAPI(t, true)
	// The shared helper leaves IPv6 unset; give the config a ULA so the
	// allowed_ips and server_ip_v6 assertions are meaningful.
	a.cfg.Subnet6 = "fd42:f1b2::/64"
	a.cfg.ServerIP6 = "fd42:f1b2::1"
	h := seedHierarchy(t, a, st) // ownUser(1 dev) + foreignUsr(1 dev)
	ensureLocalNode(t, a, st)
	edgeID, token := createNodeViaAPI(t, a, "edge", "edge.example.com")
	key, _ := wgtypes.GeneratePrivateKey()
	registerNode(t, a, token, key)

	// An xray device for the uuid side of the response.
	xu := &store.User{Username: "xray-user"}
	if err := st.CreateUser(xu); err != nil {
		t.Fatalf("seed xray user: %v", err)
	}
	if err := st.CreateDevice(&store.Device{UserID: xu.ID, Name: "pixel", Protocol: store.ProtocolXray, PublicKey: "uuid-1", IPv4: "10.8.0.9", Enabled: true}); err != nil {
		t.Fatalf("seed xray device: %v", err)
	}
	// Expire the foreign reseller: their user's device must drop out.
	past := time.Now().Add(-time.Hour).Unix()
	if err := st.SetUserExpiry(h.foreign.ID, &past); err != nil {
		t.Fatalf("expire foreign: %v", err)
	}

	rec := nodeReq(a, token, http.MethodPost, "/api/node/sync",
		`{"revision":0,"stats":[{"device_id":`+itoa(h.ownDev.ID)+`,"last_handshake":`+itoa(time.Now().Unix())+`,"rx":100,"tx":200}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", rec.Code, rec.Body.String())
	}
	var resp node.SyncResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad sync json: %v %s", err, rec.Body.String())
	}

	// Peers: exactly the live wg device (expired branch's device dropped).
	if len(resp.Peers) != 1 {
		t.Fatalf("expected 1 peer (expired branch dropped), got %+v", resp.Peers)
	}
	peer := resp.Peers[0]
	if peer.DeviceID != h.ownDev.ID || peer.PublicKey != h.ownDev.PublicKey || peer.PresharedKey != h.ownDev.PresharedKey {
		t.Fatalf("peer payload mismatch: %+v", peer)
	}
	if len(peer.AllowedIPs) < 2 ||
		!strings.HasPrefix(peer.AllowedIPs[0], "10.8.0.") || !strings.HasSuffix(peer.AllowedIPs[0], "/32") ||
		peer.AllowedIPs[1] != "fd42:f1b2::2/128" {
		t.Fatalf("allowed_ips must carry the v4 /32 and the derived v6 /128: %+v", peer.AllowedIPs)
	}
	// Xray uuids ride along.
	if len(resp.XrayUUIDs) != 1 || resp.XrayUUIDs[0] != "uuid-1" {
		t.Fatalf("xray uuids: %+v", resp.XrayUUIDs)
	}
	// Server addresses for the agent's own interface.
	if resp.ServerIPv4 == "" || resp.ServerIPv6 == "" || resp.Status != store.NodeReady {
		t.Fatalf("sync response basics: %+v", resp)
	}
	if resp.XraySNI == "" || resp.XrayDest == "" {
		t.Fatalf("xray sni/dest must be delivered when xray is enabled: %+v", resp)
	}

	// Stats landed on the device.
	d, _ := st.GetDevice(h.ownDev.ID)
	if d.LastHandshake.IsZero() || d.TotalRx != 100 || d.TotalTx != 200 {
		t.Fatalf("stats not folded: %+v", d)
	}
	// Unknown device ids are ignored silently (peer just deleted).
	rec = nodeReq(a, token, http.MethodPost, "/api/node/sync", `{"revision":1,"stats":[{"device_id":424242,"rx":1,"tx":1}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync with unknown device: %d %s", rec.Code, rec.Body.String())
	}
	// last_seen advanced.
	n, _ := st.GetNode(edgeID)
	if n.LastSeenAt == nil || time.Since(*n.LastSeenAt) > 5*time.Second {
		t.Fatalf("last_seen not stamped: %+v", n.LastSeenAt)
	}

	// Wrong token on sync: 401.
	if rec := nodeReq(a, "nope", http.MethodPost, "/api/node/sync", `{}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("sync wrong token: %d", rec.Code)
	}

	// Block the node: status blocked, empty peers, empty uuids.
	adminReq(a, http.MethodPatch, "/api/admin/nodes/"+itoa(edgeID), `{"status":"blocked"}`)
	rec = nodeReq(a, token, http.MethodPost, "/api/node/sync", `{"revision":5,"stats":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync blocked: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad blocked json: %v", err)
	}
	if resp.Status != store.NodeBlocked || len(resp.Peers) != 0 || len(resp.XrayUUIDs) != 0 {
		t.Fatalf("blocked node must get empty sets: %+v", resp)
	}
}

// TestTokenRotationInvalidatesOld: rotating returns a new token once and the
// old one stops authenticating.
func TestTokenRotationInvalidatesOld(t *testing.T) {
	a, st := newTestAPI(t, false)
	seedHierarchy(t, a, st)
	ensureLocalNode(t, a, st)
	edgeID, token := createNodeViaAPI(t, a, "edge", "edge.example.com")

	rec := adminReq(a, http.MethodPost, "/api/admin/nodes/"+itoa(edgeID)+"/token", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" || out.Token == token {
		t.Fatalf("rotate response: %v %s", err, rec.Body.String())
	}
	if rec := nodeReq(a, token, http.MethodPost, "/api/node/sync", `{}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old token must die: %d", rec.Code)
	}
	if rec := nodeReq(a, out.Token, http.MethodPost, "/api/node/sync", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("new token must work: %d", rec.Code)
	}
}
