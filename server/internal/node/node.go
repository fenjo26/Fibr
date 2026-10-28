// Package node holds the wire types shared by every party of Fibr's
// hub-and-spoke multi-node mode (see docs/multinode.md): the control plane
// (internal/api renders configs and serves the agent API), the protocol
// managers (internal/wg, internal/xray render client credentials for a given
// endpoint) and the remote agent binary (cmd/fibr-node). Keeping the shapes
// here prevents drift between what the control plane serves and what the
// agent parses; the package deliberately depends on nothing.
package node

// Endpoint is the resolved active node — the values that go into rendered
// client configs. IsLocal marks the built-in node on the control-plane box;
// for it the live manager keys are authoritative (kept in the store row too,
// but the managers own the source of truth in settings).
type Endpoint struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	WGPort         int    `json:"wg_port"`
	XrayPort       int    `json:"xray_port"`
	WGServerPubKey string `json:"wg_server_public_key"`
	XrayPubKeyB64  string `json:"xray_reality_public_key,omitempty"`
	XrayShortID    string `json:"xray_short_id,omitempty"`
	XraySNI        string `json:"xray_sni,omitempty"`
	IsLocal        bool   `json:"is_local"`
}

// Peer is one WireGuard peer delivered to a node agent in the sync response.
// DeviceID rides along so the agent can report per-peer stats back without
// knowing the control plane's database.
type Peer struct {
	DeviceID     int64    `json:"device_id"`
	PublicKey    string   `json:"public_key"`
	PresharedKey string   `json:"psk"`
	AllowedIPs   []string `json:"allowed_ips"` // "10.8.0.42/32", "fd42:f1b2::2a/128", ...
}

// PeerStats is one device's live WireGuard counters as reported by an agent.
// Rx/Tx are the raw wgctrl transfer bytes (monotonic per interface life);
// LastHandshake is unix seconds, 0 = never.
type PeerStats struct {
	DeviceID      int64 `json:"device_id"`
	LastHandshake int64 `json:"last_handshake"`
	Rx            int64 `json:"rx"`
	Tx            int64 `json:"tx"`
}

// RegisterRequest is the body of POST /api/node/register: the agent's
// generated public keys. The WireGuard key is mandatory; the Reality fields
// are present only when the node runs xray.
type RegisterRequest struct {
	ServerPublicKey      string  `json:"server_public_key"`
	XrayRealityPublicKey *string `json:"xray_reality_public_key,omitempty"`
	XrayShortID          *string `json:"xray_short_id,omitempty"`
}

// RegisterResponse tells the agent its node id and name.
type RegisterResponse struct {
	NodeID int64  `json:"node_id"`
	Name   string `json:"name"`
}

// SyncRequest is the body of POST /api/node/sync: the agent's last applied
// revision and its current peer stats.
type SyncRequest struct {
	Revision int64       `json:"revision"`
	Stats    []PeerStats `json:"stats"`
}

// SyncResponse is the control plane's answer. Status "blocked" comes with
// empty peers — the agent must then REMOVE every peer from its interface
// (that is how blocking cuts a node off without deploying anything to it).
// ServerIPv4/IPv6 are the tunnel addresses of the node's own interface.
// XraySNI/XrayDest are included so an xray-serving node needs no extra env.
type SyncResponse struct {
	Status     string   `json:"status"` // "ready" | "blocked"
	Revision   int64    `json:"revision"`
	Peers      []Peer   `json:"peers"`
	XrayUUIDs  []string `json:"xray_uuids"`
	ServerIPv4 string   `json:"server_ip_v4"`
	ServerIPv6 string   `json:"server_ip_v6"`
	XraySNI    string   `json:"xray_sni,omitempty"`
	XrayDest   string   `json:"xray_dest,omitempty"`
}
