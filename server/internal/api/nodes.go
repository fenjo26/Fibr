package api

// Multi-node (hub-and-spoke) endpoints; see docs/multinode.md.
//
// This box is the control plane and the built-in "local" node. Remote boxes
// run cmd/fibr-node, which registers its generated public keys and pulls the
// global peer list. Exactly one node is active: its host/port/keys go into
// rendered client configs (resolveActiveEndpoint). Switching the active node
// bumps endpoint_changed_at so Mini App clients know to re-download configs.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/fenjo26/Fibr/internal/node"
	"github.com/fenjo26/Fibr/internal/store"
)

/* ------------------------- endpoint resolution ------------------------- */

// resolveActiveEndpoint loads the active node and converts it into the
// endpoint shape used by the config renderers. For the local node the live
// manager keys are authoritative (they live in the settings table, not the
// node row); for remote nodes everything comes from the row the agent
// registered. A missing/absent active_node_id falls back to the local node,
// and a control plane that never ran EnsureLocalNode degrades to the config
// env values (nodeID 0) instead of failing requests.
func (a *API) resolveActiveEndpoint() (node.Endpoint, int64, error) {
	id, err := a.store.ActiveNodeID()
	if err != nil {
		return node.Endpoint{}, 0, err
	}
	var n *store.Node
	if id != 0 {
		if n, err = a.store.GetNode(id); err != nil && !errors.Is(err, store.ErrNotFound) {
			return node.Endpoint{}, 0, err
		}
		// A stale active_node_id pointing at a deleted row (GetNode returns
		// nil, ErrNotFound) falls through to the local node below.
	}
	if n == nil {
		if n, err = a.store.GetNodeByName(store.LocalNodeName); err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return node.Endpoint{}, 0, err
			}
			// Never ran EnsureLocalNode (unit tests, exotic setups): render
			// straight from env, nodeID 0.
			return node.Endpoint{
				Name: store.LocalNodeName, IsLocal: true,
				Host: a.cfg.WGHost, WGPort: a.cfg.WGPort, XrayPort: a.cfg.XrayPort,
				WGServerPubKey: a.wg.PublicKey(), XraySNI: a.cfg.XraySNI,
				XrayPubKeyB64: a.xrayPublicKey(), XrayShortID: a.cfg.XrayShortID,
			}, 0, nil
		}
		id = n.ID
	}
	ep := node.Endpoint{
		Name:           n.Name,
		Host:           n.Host,
		WGPort:         n.WGPort,
		WGServerPubKey: derefString(n.ServerPublicKey),
		XrayPubKeyB64:  derefString(n.XrayRealityPublicKey),
		XrayShortID:    derefString(n.XrayShortID),
		XraySNI:        a.cfg.XraySNI,
	}
	if n.XrayPort != nil {
		ep.XrayPort = *n.XrayPort
	}
	if n.Name == store.LocalNodeName {
		ep.IsLocal = true
		// Live managers own the local node's keys (settings table); the row
		// copies are informational.
		ep.WGServerPubKey = a.wg.PublicKey()
		if pub := a.xrayPublicKey(); pub != "" {
			ep.XrayPubKeyB64 = pub
		}
		if a.xray != nil {
			ep.XrayShortID = a.xray.ShortID()
		}
		if n.XrayPort == nil {
			ep.XrayPort = a.cfg.XrayPort
		}
	}
	return ep, id, nil
}

// xrayPublicKey safely reads the Reality public key from the xray manager
// (nil when xray is disabled).
func (a *API) xrayPublicKey() string {
	if a.xray == nil {
		return ""
	}
	return a.xray.PublicKey()
}

// endpointChangedAt reads the endpoint-changed timestamp (unix seconds,
// 0 = never changed).
func (a *API) endpointChangedAt() int64 {
	v, err := a.store.GetSettingInt(store.SettingEndpointChangedAt)
	if err != nil {
		return 0
	}
	return v
}

// bumpEndpointChanged stamps "the active endpoint just moved" so clients can
// detect a needed re-download.
func (a *API) bumpEndpointChanged() {
	if err := a.store.SetSettingInt(store.SettingEndpointChangedAt, time.Now().Unix()); err != nil {
		a.log.Error("cannot bump endpoint_changed_at", "error", err)
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

/* ------------------------- admin node endpoints ------------------------- */

// nodeOut is the JSON shape of a node in the admin panel. The agent token is
// deliberately absent: it is shown exactly once (create / rotate).
type nodeOut struct {
	ID                   int64      `json:"id"`
	Name                 string     `json:"name"`
	Host                 string     `json:"host"`
	WGPort               int        `json:"wg_port"`
	XrayPort             *int       `json:"xray_port"`
	ServerPublicKey      *string    `json:"server_public_key"`
	XrayRealityPublicKey *string    `json:"xray_reality_public_key"`
	XrayShortID          *string    `json:"xray_short_id"`
	Status               string     `json:"status"` // pending | ready | blocked
	LastSeenAt           *time.Time `json:"last_seen_at"`
	CreatedAt            time.Time  `json:"created_at"`
	IsActive             bool       `json:"is_active"`
	Online               bool       `json:"online"` // last sync within 70s (local is always on)
	Peers                int        `json:"peers"`  // active wg-protocol devices this node would serve
}

// nodeOnlineWindow: agents sync every ~20s; 70s = missed 3 ticks.
const nodeOnlineWindow = 70 * time.Second

func (a *API) newNodeOut(n store.Node, activeID int64, activeWGPeers int) nodeOut {
	out := nodeOut{
		ID: n.ID, Name: n.Name, Host: n.Host, WGPort: n.WGPort, XrayPort: n.XrayPort,
		ServerPublicKey: n.ServerPublicKey, XrayRealityPublicKey: n.XrayRealityPublicKey,
		XrayShortID: n.XrayShortID, Status: n.Status, LastSeenAt: n.LastSeenAt,
		CreatedAt: n.CreatedAt, IsActive: n.ID == activeID, Peers: activeWGPeers,
	}
	out.Online = n.Name == store.LocalNodeName ||
		(n.LastSeenAt != nil && time.Since(*n.LastSeenAt) < nodeOnlineWindow)
	return out
}

// fetchNodeSuper loads the node for an admin-node endpoint and enforces the
// superadmin-only rule (infrastructure is the installation owner's).
func (a *API) fetchNodeSuper(w http.ResponseWriter, r *http.Request) (*store.Node, bool) {
	if !ident(r).isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return nil, false
	}
	id, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return nil, false
	}
	n, err := a.store.GetNode(id)
	if err != nil {
		a.errJSON(w, http.StatusNotFound, "node not found")
		return nil, false
	}
	return n, true
}

// countActiveWGPeers returns how many active wg-protocol devices exist — the
// peer count every node would serve (peers are global).
func (a *API) countActiveWGPeers() (int, error) {
	devices, err := a.store.ListActiveDevices(time.Now())
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range devices {
		if d.Protocol == store.ProtocolWG {
			n++
		}
	}
	return n, nil
}

func (a *API) handleAdminListNodes(w http.ResponseWriter, r *http.Request) {
	if !ident(r).isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return
	}
	nodes, err := a.store.ListNodes()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	activeID, err := a.store.ActiveNodeID()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	// Active id 0 means "local, never set": resolve to the actual row id so
	// is_active flags render correctly.
	if activeID == 0 {
		if local, err := a.store.GetNodeByName(store.LocalNodeName); err == nil {
			activeID = local.ID
		}
	}
	peers, err := a.countActiveWGPeers()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	out := make([]nodeOut, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, a.newNodeOut(n, activeID, peers))
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out, "active_node_id": activeID})
}

// newAgentToken generates the bearer token a remote node authenticates with:
// 32 random bytes, base64url (43 chars).
func newAgentToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (a *API) handleAdminCreateNode(w http.ResponseWriter, r *http.Request) {
	if !ident(r).isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return
	}
	var body struct {
		Name     string `json:"name"`
		Host     string `json:"host"`
		WGPort   *int   `json:"wg_port"`
		XrayPort *int   `json:"xray_port"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	name := sanitizeName(body.Name, "")
	if name == "" || body.Host == "" {
		a.errJSON(w, http.StatusBadRequest, "name and host are required")
		return
	}
	wgPort := 51820
	if body.WGPort != nil {
		if *body.WGPort < 1 || *body.WGPort > 65535 {
			a.errJSON(w, http.StatusBadRequest, "wg_port out of range")
			return
		}
		wgPort = *body.WGPort
	}
	if body.XrayPort != nil && (*body.XrayPort < 1 || *body.XrayPort > 65535) {
		a.errJSON(w, http.StatusBadRequest, "xray_port out of range")
		return
	}
	token, err := newAgentToken()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	n := &store.Node{Name: name, Host: body.Host, WGPort: wgPort, XrayPort: body.XrayPort, Token: token}
	if err := a.store.CreateNode(n); err != nil {
		a.errJSON(w, http.StatusBadRequest, "node name already exists")
		return
	}
	a.log.Info("node created", "node_id", n.ID, "name", n.Name, "host", n.Host)
	// The token is returned exactly once — the panel shows it for the operator
	// to paste into the node's FIBR_NODE_TOKEN.
	writeJSON(w, http.StatusCreated, map[string]any{
		"node":  a.newNodeOut(*n, 0, 0),
		"token": token,
	})
}

func (a *API) handleAdminUpdateNode(w http.ResponseWriter, r *http.Request) {
	n, ok := a.fetchNodeSuper(w, r)
	if !ok {
		return
	}
	var body struct {
		Name     *string `json:"name"`
		Host     *string `json:"host"`
		WGPort   *int    `json:"wg_port"`
		XrayPort *int    `json:"xray_port"`
		Status   *string `json:"status"` // "ready" | "blocked"
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	isLocal := n.Name == store.LocalNodeName
	activeID, err := a.store.ActiveNodeID()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	if activeID == 0 {
		activeID = n.ID // never set: local is implicitly active
	}

	// Status first: blocking the active node would leave every client config
	// rendering against a node we just told agents to empty out.
	if body.Status != nil {
		switch *body.Status {
		case store.NodeBlocked:
			if n.ID == activeID {
				a.errJSON(w, http.StatusBadRequest, "activate another node first")
				return
			}
			if err := a.store.SetNodeStatus(n.ID, store.NodeBlocked); err != nil {
				a.errJSON(w, http.StatusInternalServerError, "storage error")
				return
			}
			// Bump the revision: the blocked node's agent sees the new one on
			// its next sync and clears its peers.
			if _, err := a.store.BumpPeerRevision(); err != nil {
				a.log.Error("peer revision bump failed", "error", err)
			}
			a.log.Info("node blocked", "node_id", n.ID)
		case store.NodeReady:
			if err := a.store.SetNodeStatus(n.ID, store.NodeReady); err != nil {
				a.errJSON(w, http.StatusInternalServerError, "storage error")
				return
			}
			a.log.Info("node unblocked", "node_id", n.ID)
		default:
			a.errJSON(w, http.StatusBadRequest, "status must be ready or blocked")
			return
		}
	}

	// Connectivity attrs. The local node is a mirror of env, not a free-form
	// row — host/port edits would be silently reverted on next boot.
	if isLocal && (body.Host != nil || body.WGPort != nil) {
		a.errJSON(w, http.StatusBadRequest, "local node follows FIBR_WG_HOST")
		return
	}
	attrsChanged := false
	if body.Host != nil && *body.Host != n.Host {
		if err := a.store.UpdateNodeAttrs(n.ID, *body.Host, n.WGPort, n.XrayPort); err != nil {
			a.errJSON(w, http.StatusInternalServerError, "storage error")
			return
		}
		attrsChanged = true
	}
	if body.WGPort != nil {
		if *body.WGPort < 1 || *body.WGPort > 65535 {
			a.errJSON(w, http.StatusBadRequest, "wg_port out of range")
			return
		}
		xp := n.XrayPort
		var host string
		if body.Host != nil {
			host = *body.Host
		} else {
			host = n.Host
		}
		if err := a.store.UpdateNodeAttrs(n.ID, host, *body.WGPort, xp); err != nil {
			a.errJSON(w, http.StatusInternalServerError, "storage error")
			return
		}
		attrsChanged = true
	}
	if body.XrayPort != nil {
		if *body.XrayPort < 1 || *body.XrayPort > 65535 {
			a.errJSON(w, http.StatusBadRequest, "xray_port out of range")
			return
		}
		host, wgPort := n.Host, n.WGPort
		if body.Host != nil {
			host = *body.Host
		}
		if body.WGPort != nil {
			wgPort = *body.WGPort
		}
		if err := a.store.UpdateNodeAttrs(n.ID, host, wgPort, body.XrayPort); err != nil {
			a.errJSON(w, http.StatusInternalServerError, "storage error")
			return
		}
		attrsChanged = true
	}
	if body.Name != nil {
		name := sanitizeName(*body.Name, "")
		if name == "" {
			a.errJSON(w, http.StatusBadRequest, "name must not be empty")
			return
		}
		if name != n.Name {
			if err := a.store.SetNodeName(n.ID, name); err != nil {
				a.errJSON(w, http.StatusBadRequest, "node name already exists")
				return
			}
		}
	}

	// Host/port edits on the ACTIVE node change rendered client configs.
	if attrsChanged && n.ID == activeID {
		a.bumpEndpointChanged()
	}

	updated, err := a.store.GetNode(n.ID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	peers, _ := a.countActiveWGPeers()
	writeJSON(w, http.StatusOK, a.newNodeOut(*updated, activeID, peers))
}

func (a *API) handleAdminDeleteNode(w http.ResponseWriter, r *http.Request) {
	n, ok := a.fetchNodeSuper(w, r)
	if !ok {
		return
	}
	if n.Name == store.LocalNodeName {
		a.errJSON(w, http.StatusBadRequest, "the local node cannot be deleted")
		return
	}
	activeID, err := a.store.ActiveNodeID()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	if activeID == 0 || n.ID == activeID {
		a.errJSON(w, http.StatusBadRequest, "activate another node first")
		return
	}
	if err := a.store.DeleteNode(n.ID); err != nil {
		a.errJSON(w, http.StatusNotFound, "node not found")
		return
	}
	a.log.Info("node deleted", "node_id", n.ID, "name", n.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleAdminActivateNode(w http.ResponseWriter, r *http.Request) {
	n, ok := a.fetchNodeSuper(w, r)
	if !ok {
		return
	}
	// The local node is always activatable (it is this box, always ready);
	// remote nodes must have completed registration.
	if n.Name != store.LocalNodeName && n.Status != store.NodeReady {
		a.errJSON(w, http.StatusBadRequest, "node is not ready")
		return
	}
	if err := a.store.SetActiveNodeID(n.ID); err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	a.bumpEndpointChanged()
	// Issued configs now point at this node; remote agents pick the peer list
	// up on their next sync (peers are global, nothing else to push).
	a.log.Info("active node switched", "node_id", n.ID, "name", n.Name)
	updated, err := a.store.GetNode(n.ID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	peers, _ := a.countActiveWGPeers()
	writeJSON(w, http.StatusOK, a.newNodeOut(*updated, n.ID, peers))
}

// handleAdminRotateNodeToken issues a fresh agent token (leak remediation).
// The new token is returned exactly once and invalidates the old one
// immediately.
func (a *API) handleAdminRotateNodeToken(w http.ResponseWriter, r *http.Request) {
	n, ok := a.fetchNodeSuper(w, r)
	if !ok {
		return
	}
	if n.Name == store.LocalNodeName {
		a.errJSON(w, http.StatusBadRequest, "the local node has no agent token")
		return
	}
	token, err := newAgentToken()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if err := a.store.SetNodeToken(n.ID, token); err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	a.log.Info("node token rotated", "node_id", n.ID, "name", n.Name)
	activeID, _ := a.store.ActiveNodeID()
	peers, _ := a.countActiveWGPeers()
	writeJSON(w, http.StatusOK, map[string]any{
		"node":  a.newNodeOut(*n, activeID, peers),
		"token": token,
	})
}

/* ------------------------- node agent endpoints ------------------------- */

// nodeAuth authenticates a remote fibr-node by its bearer token. Tokens are
// compared by SHA-256 digests through subtle.ConstantTimeCompare so the
// comparison leaks nothing about the expected value's length or content.
// The local node's placeholder token is rejected explicitly (no agent runs
// on the control-plane box under this identity).
func (a *API) nodeAuth(next func(http.ResponseWriter, *http.Request, *store.Node)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" || store.LocalNodeTokenPlaceholder(token) {
			a.errJSON(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		nodes, err := a.store.ListNodes()
		if err != nil {
			a.errJSON(w, http.StatusInternalServerError, "storage error")
			return
		}
		got := sha256.Sum256([]byte(token))
		var match *store.Node
		for i := range nodes {
			if nodes[i].Name == store.LocalNodeName {
				continue // defensive: never authenticate as the local node
			}
			want := sha256.Sum256([]byte(nodes[i].Token))
			if subtle.ConstantTimeCompare(got[:], want[:]) == 1 {
				match = &nodes[i]
				break
			}
		}
		if match == nil {
			a.errJSON(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r, match)
	}
}

// handleNodeRegister completes a node's enrolment: the agent posts the
// WireGuard (and optional Reality) public keys it generated on first boot.
// Idempotent for the same key; a different key is a hard 409 — the issued
// client configs embed the stored key, so silently accepting a new one would
// break every credential ever rendered for this node.
func (a *API) handleNodeRegister(w http.ResponseWriter, r *http.Request, n *store.Node) {
	var body node.RegisterRequest
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.ServerPublicKey == "" {
		a.errJSON(w, http.StatusBadRequest, "server_public_key is required")
		return
	}
	now := time.Now().Unix()
	switch {
	case n.Status == store.NodeBlocked:
		a.errJSON(w, http.StatusForbidden, "node is blocked")
		return
	case n.Status == store.NodeReady && n.ServerPublicKey != nil:
		if *n.ServerPublicKey != body.ServerPublicKey {
			a.errJSON(w, http.StatusConflict, "key mismatch: delete and re-create the node to re-key it")
			return
		}
		// Same key: a plain re-boot of an already registered agent.
		_ = a.store.SetNodeLastSeen(n.ID, now)
	default:
		// pending (first boot) or a ready row without keys (created before
		// registration completed): store them and finish enrolment.
		if err := a.store.UpdateNodeKeys(n.ID, body.ServerPublicKey, body.XrayRealityPublicKey, body.XrayShortID); err != nil {
			a.errJSON(w, http.StatusInternalServerError, "storage error")
			return
		}
		if n.Status != store.NodeReady {
			if err := a.store.SetNodeStatus(n.ID, store.NodeReady); err != nil {
				a.errJSON(w, http.StatusInternalServerError, "storage error")
				return
			}
		}
		_ = a.store.SetNodeLastSeen(n.ID, now)
		a.log.Info("node registered", "node_id", n.ID, "name", n.Name)
	}
	writeJSON(w, http.StatusOK, node.RegisterResponse{NodeID: n.ID, Name: n.Name})
}

// handleNodeSync is the agent's pull loop: it reports live peer stats and
// receives the full desired peer set. A blocked node receives status
// "blocked" with empty peers — the agent must then remove every peer, which
// is how blocking cuts a node off without logging into it.
func (a *API) handleNodeSync(w http.ResponseWriter, r *http.Request, n *store.Node) {
	var body node.SyncRequest
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = a.store.SetNodeLastSeen(n.ID, time.Now().Unix())

	if n.Status == store.NodeBlocked {
		resp := node.SyncResponse{
			Status:     store.NodeBlocked,
			Revision:   a.currentRevision(),
			Peers:      []node.Peer{},
			XrayUUIDs:  []string{},
			ServerIPv4: a.cfg.ServerIP, ServerIPv6: a.cfg.ServerIP6,
		}
		if a.cfg.XrayEnabled {
			// Included even for blocked nodes: the agent rewrites its xray
			// config with an EMPTY client list, which still needs valid
			// camouflage fields.
			resp.XraySNI = a.cfg.XraySNI
			resp.XrayDest = a.cfg.XrayDest
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Fold the agent's stats into the devices. The agent reports raw wgctrl
	// counters; totals are accumulated with counter-reset detection exactly
	// like the local CollectStats. Known caveat: during a node switch BOTH
	// the old and the new node legitimately serve the same peers, so each
	// reporter's deltas are computed against the other's counters — traffic
	// totals may double-count until clients re-download (handshakes/online
	// stay correct). Unknown device ids are ignored silently (the agent may
	// briefly hold a peer we just deleted).
	for _, s := range body.Stats {
		d, err := a.store.GetDevice(s.DeviceID)
		if err != nil {
			continue
		}
		dRx, dTx := s.Rx-d.LastRx, s.Tx-d.LastTx
		if dRx < 0 {
			dRx = s.Rx // counters were reset (agent restart)
		}
		if dTx < 0 {
			dTx = s.Tx
		}
		var hs time.Time
		if s.LastHandshake > 0 {
			hs = time.Unix(s.LastHandshake, 0)
		}
		if err := a.store.UpdateDeviceStats(d.ID, hs, s.Rx, s.Tx, d.TotalRx+dRx, d.TotalTx+dTx); err != nil {
			a.log.Warn("node stats: cannot update device", "device_id", d.ID, "error", err)
		}
	}

	// The desired peer set: every active wg device (peers are global —
	// ListActiveDevices already drops disabled and expired accounts).
	devices, err := a.store.ListActiveDevices(time.Now())
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	peers := make([]node.Peer, 0, len(devices))
	uuids := make([]string, 0)
	for _, d := range devices {
		switch d.Protocol {
		case store.ProtocolWG:
			allowed := []string{d.IPv4 + "/32"}
			if ip6 := a.cfg.IPv6For(d.IPv4); ip6 != "" {
				allowed = append(allowed, ip6+"/128")
			}
			peers = append(peers, node.Peer{
				DeviceID:     d.ID,
				PublicKey:    d.PublicKey,
				PresharedKey: d.PresharedKey,
				AllowedIPs:   allowed,
			})
		case store.ProtocolXray:
			uuids = append(uuids, d.PublicKey)
		}
	}

	resp := node.SyncResponse{
		Status:     store.NodeReady,
		Revision:   a.currentRevision(),
		Peers:      peers,
		XrayUUIDs:  uuids,
		ServerIPv4: a.cfg.ServerIP, ServerIPv6: a.cfg.ServerIP6,
	}
	if a.cfg.XrayEnabled {
		resp.XraySNI = a.cfg.XraySNI
		resp.XrayDest = a.cfg.XrayDest
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) currentRevision() int64 {
	v, err := a.store.GetSettingInt(store.SettingPeerRevision)
	if err != nil {
		return 0
	}
	return v
}
