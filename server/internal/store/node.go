package store

// Nodes: the multi-node registry (hub-and-spoke; see docs/multinode.md).
// This box is the control plane and always carries a built-in "local" node;
// remote boxes run cmd/fibr-node, register their public keys and pull the
// global peer list. Exactly one node is "active" (settings key
// active_node_id) — its host/ports/keys go into rendered client configs.

import (
	"database/sql"
	"errors"
	"time"
)

// Node status values.
const (
	NodePending = "pending" // created, agent has not registered its keys yet
	NodeReady   = "ready"   // keys known, may be activated
	NodeBlocked = "blocked" // administratively off: agents get an empty peer list
)

// LocalNodeName is the name of the built-in node row representing this box.
const LocalNodeName = "local"

// localNodeToken fills the NOT NULL UNIQUE token column of the built-in node.
// No agent ever learns it: it is never returned by any endpoint, and the node
// auth middleware explicitly rejects the local node row, so the value only
// has to be unique — not secret.
const localNodeToken = "local-node-token-00000000-not-an-agent-token"

// Node is one entry of the multi-node registry. Remote agents authenticate
// with Token (generated at creation, shown once); server_public_key and the
// xray_* fields are the keys the agent generated on its first boot — client
// configs embed them when the node is active, so they are immutable after
// registration (changing them would break every issued credential).
type Node struct {
	ID                   int64      `json:"id"`
	Name                 string     `json:"name"`
	Host                 string     `json:"host"`
	WGPort               int        `json:"wg_port"`
	XrayPort             *int       `json:"xray_port"` // nil = xray not served by this node
	Token                string     `json:"-"`         // never serialized into API responses
	ServerPublicKey      *string    `json:"server_public_key"`
	XrayRealityPublicKey *string    `json:"xray_reality_public_key"`
	XrayShortID          *string    `json:"xray_short_id"`
	Status               string     `json:"status"` // pending | ready | blocked
	LastSeenAt           *time.Time `json:"last_seen_at"`
	CreatedAt            time.Time  `json:"created_at"`
}

const nodeCols = `id, name, host, wg_port, xray_port, token, server_public_key, xray_reality_public_key, xray_short_id, status, last_seen_at, created_at`

func scanNode(r interface{ Scan(...any) error }) (*Node, error) {
	var n Node
	var xrayPort sql.NullInt64
	var serverPub, xrayPub, xraySID sql.NullString
	var lastSeen sql.NullInt64
	var created int64
	if err := r.Scan(&n.ID, &n.Name, &n.Host, &n.WGPort, &xrayPort, &n.Token,
		&serverPub, &xrayPub, &xraySID, &n.Status, &lastSeen, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if xrayPort.Valid {
		p := int(xrayPort.Int64)
		n.XrayPort = &p
	}
	if serverPub.Valid {
		v := serverPub.String
		n.ServerPublicKey = &v
	}
	if xrayPub.Valid {
		v := xrayPub.String
		n.XrayRealityPublicKey = &v
	}
	if xraySID.Valid {
		v := xraySID.String
		n.XrayShortID = &v
	}
	if lastSeen.Valid && lastSeen.Int64 > 0 {
		t := time.Unix(lastSeen.Int64, 0)
		n.LastSeenAt = &t
	}
	n.CreatedAt = time.Unix(created, 0)
	return &n, nil
}

// CreateNode inserts a node; ID and CreatedAt are filled in. Status defaults
// to pending; the token is chosen by the caller (random for remote nodes,
// the fixed placeholder for the local one).
func (s *Store) CreateNode(n *Node) error {
	if n.Status == "" {
		n.Status = NodePending
	}
	now := time.Now().Unix()
	var xrayPort any
	if n.XrayPort != nil {
		xrayPort = *n.XrayPort
	}
	res, err := s.db.Exec(
		`INSERT INTO nodes (name, host, wg_port, xray_port, token, server_public_key, xray_reality_public_key, xray_short_id, status, last_seen_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Name, n.Host, n.WGPort, xrayPort, n.Token,
		nullStr(n.ServerPublicKey), nullStr(n.XrayRealityPublicKey), nullStr(n.XrayShortID),
		n.Status, nil, now,
	)
	if err != nil {
		return err
	}
	n.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	n.CreatedAt = time.Unix(now, 0)
	return nil
}

func nullStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// GetNode returns a node by id, or ErrNotFound.
func (s *Store) GetNode(id int64) (*Node, error) {
	row := s.db.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE id = ?`, id)
	return scanNode(row)
}

// GetNodeByName returns a node by its unique name, or ErrNotFound.
func (s *Store) GetNodeByName(name string) (*Node, error) {
	row := s.db.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE name = ?`, name)
	return scanNode(row)
}

// GetNodeByToken returns a node by its agent token, or ErrNotFound. Used by
// the node-agent auth middleware, which rejects the local node afterwards.
func (s *Store) GetNodeByToken(token string) (*Node, error) {
	row := s.db.QueryRow(`SELECT `+nodeCols+` FROM nodes WHERE token = ?`, token)
	return scanNode(row)
}

// ListNodes returns all nodes ordered by id.
func (s *Store) ListNodes() ([]Node, error) {
	rows, err := s.db.Query(`SELECT ` + nodeCols + ` FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// UpdateNodeKeys stores the node's WireGuard/Xray public keys (delivered by
// the agent's first register call). Keys are write-once from the API's point
// of view; this method exists for the registration path only.
func (s *Store) UpdateNodeKeys(id int64, serverPub string, xrayPub, xraySID *string) error {
	res, err := s.db.Exec(
		`UPDATE nodes SET server_public_key = ?, xray_reality_public_key = ?, xray_short_id = ? WHERE id = ?`,
		serverPub, nullStr(xrayPub), nullStr(xraySID), id,
	)
	return touched(res, err)
}

// SetNodeStatus transitions a node between pending/ready/blocked.
func (s *Store) SetNodeStatus(id int64, status string) error {
	res, err := s.db.Exec(`UPDATE nodes SET status = ? WHERE id = ?`, status, id)
	return touched(res, err)
}

// SetNodeLastSeen stamps the agent's last successful API call.
func (s *Store) SetNodeLastSeen(id int64, unix int64) error {
	res, err := s.db.Exec(`UPDATE nodes SET last_seen_at = ? WHERE id = ?`, unix, id)
	return touched(res, err)
}

// UpdateNodeAttrs updates the mutable connectivity attributes. The local
// node row is refreshed from env on every boot through this method.
func (s *Store) UpdateNodeAttrs(id int64, host string, wgPort int, xrayPort *int) error {
	var xp any
	if xrayPort != nil {
		xp = *xrayPort
	}
	res, err := s.db.Exec(`UPDATE nodes SET host = ?, wg_port = ?, xray_port = ? WHERE id = ?`, host, wgPort, xp, id)
	return touched(res, err)
}

// SetNodeToken replaces the agent token (rotate-on-leak path). Returns the
// stored row unchanged otherwise.
func (s *Store) SetNodeToken(id int64, token string) error {
	res, err := s.db.Exec(`UPDATE nodes SET token = ? WHERE id = ?`, token, id)
	return touched(res, err)
}

// SetNodeName renames a node (unique; collisions error).
func (s *Store) SetNodeName(id int64, name string) error {
	res, err := s.db.Exec(`UPDATE nodes SET name = ? WHERE id = ?`, name, id)
	return touched(res, err)
}

// DeleteNode removes a node. Callers must refuse to delete the active or the
// local node; peers are global (they live on devices), so deleting an
// inactive node orphans nothing.
func (s *Store) DeleteNode(id int64) error {
	res, err := s.db.Exec(`DELETE FROM nodes WHERE id = ?`, id)
	return touched(res, err)
}

// ActiveNodeID returns the id of the node whose endpoint goes into rendered
// client configs; 0 means "unset" (the caller falls back to the local node).
func (s *Store) ActiveNodeID() (int64, error) {
	return s.GetSettingInt(SettingActiveNodeID)
}

// SetActiveNodeID switches the active node (the caller bumps
// endpoint_changed_at so clients notice).
func (s *Store) SetActiveNodeID(id int64) error {
	return s.SetSettingInt(SettingActiveNodeID, id)
}

// EnsureLocalNode upserts the built-in "local" node row, refreshing
// host/ports from the current configuration (env edits land on next boot),
// and defaults the active node to it when no active node was ever chosen.
// Called from cmd/fibr after Open and from tests. The returned id is the
// local node's row id.
func EnsureLocalNode(st *Store, host string, wgPort int, xrayPort *int) (int64, error) {
	n, err := st.GetNodeByName(LocalNodeName)
	switch {
	case err == nil:
		attrsChanged := n.Host != host || n.WGPort != wgPort || !intPtrEqual(n.XrayPort, xrayPort)
		if attrsChanged {
			if err := st.UpdateNodeAttrs(n.ID, host, wgPort, xrayPort); err != nil {
				return 0, err
			}
		}
		if n.Status != NodeReady {
			// The local node cannot be pending (no agent registers it) and a
			// blocked local node would strand the whole box; the operator
			// blocks remote nodes, not the hub itself.
			if err := st.SetNodeStatus(n.ID, NodeReady); err != nil {
				return 0, err
			}
		}
	case errors.Is(err, ErrNotFound):
		n = &Node{
			Name:     LocalNodeName,
			Host:     host,
			WGPort:   wgPort,
			XrayPort: xrayPort,
			Token:    localNodeToken,
			Status:   NodeReady,
		}
		if err := st.CreateNode(n); err != nil {
			return 0, err
		}
	default:
		return 0, err
	}

	// First boot (or pre-multi-node database): the local node is active until
	// the operator switches. A deleted setting re-defaults the same way.
	if _, err := st.GetSetting(SettingActiveNodeID); errors.Is(err, ErrNotFound) {
		if err := st.SetSettingInt(SettingActiveNodeID, n.ID); err != nil {
			return 0, err
		}
	}
	return n.ID, nil
}

func intPtrEqual(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// LocalNodeTokenPlaceholder reports whether token is the built-in node's
// fixed placeholder — the auth middleware uses it as a belt-and-braces
// rejection on top of the name check.
func LocalNodeTokenPlaceholder(token string) bool { return token == localNodeToken }
