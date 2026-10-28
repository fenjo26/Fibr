package store

// Node registry + per-user device caps tests (multi-node and reseller
// limits; see docs/multinode.md and docs/billing.md).

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestEffectiveMaxDevices(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }
	cases := []struct {
		own, owner *int64
		fallback   int
		want       int
	}{
		{own: nil, owner: nil, fallback: 10, want: 10},      // global default
		{own: nil, owner: nil, fallback: 3, want: 3},        // default respects env
		{own: i64(5), owner: nil, fallback: 10, want: 5},    // own cap
		{own: i64(20), owner: nil, fallback: 10, want: 20},  // a set value is never widened by the fallback
		{own: nil, owner: i64(4), fallback: 10, want: 4},    // owner cascade
		{own: i64(8), owner: i64(4), fallback: 10, want: 4}, // min of both
		{own: i64(2), owner: i64(9), fallback: 10, want: 2},
		{own: i64(0), owner: nil, fallback: 10, want: 0}, // 0 = "no new devices" is a real value
		{own: i64(0), owner: i64(5), fallback: 10, want: 0},
	}
	for i, c := range cases {
		if got := EffectiveMaxDevices(c.own, c.owner, c.fallback); got != c.want {
			t.Errorf("case %d: EffectiveMaxDevices(%v, %v, %d) = %d, want %d", i, c.own, c.owner, c.fallback, got, c.want)
		}
	}
}

func TestSetUserMaxDevices(t *testing.T) {
	s := openTestStore(t)
	u := &User{Username: "capped"}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, _ := s.GetUser(u.ID)
	if got.MaxDevices != nil {
		t.Fatalf("fresh user must default to nil (global default), got %v", *got.MaxDevices)
	}

	five := int64(5)
	if err := s.SetUserMaxDevices(u.ID, &five); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ = s.GetUser(u.ID)
	if got.MaxDevices == nil || *got.MaxDevices != 5 {
		t.Fatalf("cap not persisted: %+v", got.MaxDevices)
	}

	if err := s.SetUserMaxDevices(u.ID, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = s.GetUser(u.ID)
	if got.MaxDevices != nil {
		t.Fatalf("nil must clear the cap, got %v", *got.MaxDevices)
	}

	zero := int64(0)
	if err := s.SetUserMaxDevices(u.ID, &zero); err != nil {
		t.Fatalf("set zero: %v", err)
	}
	got, _ = s.GetUser(u.ID)
	if got.MaxDevices == nil || *got.MaxDevices != 0 {
		t.Fatalf("zero must persist as a real value, got %+v", got.MaxDevices)
	}

	// CreateUser stores the cap on insert.
	capped := &User{Username: "born-capped", MaxDevices: &five}
	if err := s.CreateUser(capped); err != nil {
		t.Fatalf("create capped: %v", err)
	}
	got, _ = s.GetUser(capped.ID)
	if got.MaxDevices == nil || *got.MaxDevices != 5 {
		t.Fatalf("cap not stored on create: %+v", got.MaxDevices)
	}
}

func TestSettingsInt(t *testing.T) {
	s := openTestStore(t)
	if v, err := s.GetSettingInt("missing"); err != nil || v != 0 {
		t.Fatalf("missing key must read as 0/nil-error, got %d %v", v, err)
	}
	if err := s.SetSettingInt("n", 41); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v, err := s.GetSettingInt("n"); err != nil || v != 41 {
		t.Fatalf("round-trip: %d %v", v, err)
	}
	if err := s.SetSettingInt("n", -7); err != nil {
		t.Fatalf("set negative: %v", err)
	}
	if v, _ := s.GetSettingInt("n"); v != -7 {
		t.Fatalf("negative round-trip: %d", v)
	}

	// Revision bumping is monotonic from whatever the current value is.
	if v, err := s.BumpPeerRevision(); err != nil || v != 1 {
		t.Fatalf("first bump: %d %v", v, err)
	}
	if v, err := s.BumpPeerRevision(); err != nil || v != 2 {
		t.Fatalf("second bump: %d %v", v, err)
	}

	// A malformed value surfaces as an error, not silently as 0.
	if err := s.SetSetting(SettingPeerRevision, "not-a-number"); err != nil {
		t.Fatalf("seed junk: %v", err)
	}
	if _, err := s.GetSettingInt(SettingPeerRevision); err == nil {
		t.Fatal("junk value must error")
	}
}

func TestNodesCRUD(t *testing.T) {
	s := openTestStore(t)

	// Create (pending by default) + read by all three keys.
	xp := 8443
	n := &Node{Name: "edge-1", Host: "edge1.example.com", WGPort: 51820, XrayPort: &xp, Token: "tok-edge-1"}
	if err := s.CreateNode(n); err != nil {
		t.Fatalf("create: %v", err)
	}
	if n.ID == 0 || n.Status != NodePending || n.CreatedAt.IsZero() {
		t.Fatalf("create did not fill defaults: %+v", n)
	}
	got, err := s.GetNode(n.ID)
	if err != nil || got.Name != "edge-1" || got.Token != "tok-edge-1" {
		t.Fatalf("GetNode: %+v err=%v", got, err)
	}
	if got.XrayPort == nil || *got.XrayPort != 8443 {
		t.Fatalf("xray_port round-trip: %+v", got.XrayPort)
	}
	byName, err := s.GetNodeByName("edge-1")
	if err != nil || byName.ID != n.ID {
		t.Fatalf("GetNodeByName: %+v err=%v", byName, err)
	}
	byToken, err := s.GetNodeByToken("tok-edge-1")
	if err != nil || byToken.ID != n.ID {
		t.Fatalf("GetNodeByToken: %+v err=%v", byToken, err)
	}
	if _, err := s.GetNode(9999); err != ErrNotFound {
		t.Fatalf("missing node: %v", err)
	}

	// Registration: keys + status + last_seen.
	pub, xpub, xsid := "PUBKEY", "XPUB", "abcd0123"
	if err := s.UpdateNodeKeys(n.ID, pub, &xpub, &xsid); err != nil {
		t.Fatalf("keys: %v", err)
	}
	if err := s.SetNodeStatus(n.ID, NodeReady); err != nil {
		t.Fatalf("status: %v", err)
	}
	now := time.Now().Unix()
	if err := s.SetNodeLastSeen(n.ID, now); err != nil {
		t.Fatalf("last seen: %v", err)
	}
	got, _ = s.GetNode(n.ID)
	if got.ServerPublicKey == nil || *got.ServerPublicKey != pub ||
		got.XrayRealityPublicKey == nil || *got.XrayRealityPublicKey != xpub ||
		got.XrayShortID == nil || *got.XrayShortID != xsid ||
		got.Status != NodeReady || got.LastSeenAt == nil || got.LastSeenAt.Unix() < now-2 {
		t.Fatalf("registration fields: %+v", got)
	}

	// Attrs update (host change, xray cleared to NULL).
	if err := s.UpdateNodeAttrs(n.ID, "new.example.com", 51821, nil); err != nil {
		t.Fatalf("attrs: %v", err)
	}
	got, _ = s.GetNode(n.ID)
	if got.Host != "new.example.com" || got.WGPort != 51821 || got.XrayPort != nil {
		t.Fatalf("attrs not applied: %+v", got)
	}

	// Rename (unique) + token rotation.
	if err := s.SetNodeName(n.ID, "edge-one"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	n2 := &Node{Name: "edge-2", Host: "edge2.example.com", WGPort: 51820, Token: "tok-edge-2"}
	if err := s.CreateNode(n2); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if err := s.SetNodeName(n2.ID, "edge-one"); err == nil {
		t.Fatal("duplicate rename must fail")
	}
	if err := s.SetNodeToken(n.ID, "tok-rotated"); err != nil {
		t.Fatalf("rotate token: %v", err)
	}
	if got, _ := s.GetNodeByToken("tok-rotated"); got.ID != n.ID {
		t.Fatalf("rotated token must replace the old one: %+v", got)
	}
	if _, err := s.GetNodeByToken("tok-edge-1"); err != ErrNotFound {
		t.Fatalf("old token must be dead: %v", err)
	}

	// List + active switch + delete.
	nodes, err := s.ListNodes()
	if err != nil || len(nodes) != 2 {
		t.Fatalf("ListNodes: %v err=%v", nodes, err)
	}
	if err := s.SetActiveNodeID(n.ID); err != nil {
		t.Fatalf("set active: %v", err)
	}
	if id, err := s.ActiveNodeID(); err != nil || id != n.ID {
		t.Fatalf("ActiveNodeID: %d %v", id, err)
	}
	if err := s.DeleteNode(n2.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetNode(n2.ID); err != ErrNotFound {
		t.Fatalf("deleted node must be gone: %v", err)
	}
}

func TestEnsureLocalNode(t *testing.T) {
	s := openTestStore(t)

	id, err := EnsureLocalNode(s, "hub.example.com", 51820, nil)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	n, err := s.GetNode(id)
	if err != nil || n.Name != LocalNodeName || n.Status != NodeReady || n.Host != "hub.example.com" {
		t.Fatalf("local node row: %+v err=%v", n, err)
	}
	if LocalNodeTokenPlaceholder(n.Token) != true {
		t.Fatalf("local node must carry the placeholder token: %q", n.Token)
	}
	// First boot defaults the active node to local.
	if active, _ := s.ActiveNodeID(); active != id {
		t.Fatalf("active node must default to local, got %d", active)
	}

	// Second boot with changed env: host/ports refresh in place, same row.
	id2, err := EnsureLocalNode(s, "newhub.example.com", 51821, nil)
	if err != nil || id2 != id {
		t.Fatalf("re-ensure must reuse the row: %d vs %d err=%v", id2, id, err)
	}
	n, _ = s.GetNode(id)
	if n.Host != "newhub.example.com" || n.WGPort != 51821 {
		t.Fatalf("local node must follow env: %+v", n)
	}

	// The active default never overrides an explicit choice.
	other := &Node{Name: "edge", Host: "e.example.com", WGPort: 51820, Token: "tok"}
	if err := s.CreateNode(other); err != nil {
		t.Fatalf("create edge: %v", err)
	}
	if err := s.SetActiveNodeID(other.ID); err != nil {
		t.Fatalf("switch active: %v", err)
	}
	if _, err := EnsureLocalNode(s, "newhub.example.com", 51821, nil); err != nil {
		t.Fatalf("third ensure: %v", err)
	}
	if active, _ := s.ActiveNodeID(); active != other.ID {
		t.Fatalf("ensure must not steal an explicit active node, got %d", active)
	}
}

// TestMaxDevicesMigration verifies the pragma-gated users.max_devices
// migration: a database from before the caps feature (roles + expiry schema,
// no max_devices) upgrades in place, existing rows read back as NULL, and a
// repeated Open is a no-op that keeps values written after the migration.
func TestMaxDevicesMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-caps.db")
	oldSchema := `
	CREATE TABLE IF NOT EXISTS users (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id INTEGER UNIQUE,
		username    TEXT    NOT NULL DEFAULT '',
		role        TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin','superadmin')),
		owner_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
		disabled    INTEGER NOT NULL DEFAULT 0,
		expires_at  INTEGER,
		created_at  INTEGER NOT NULL
	);`
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("old schema: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO users (telegram_id, username, role, created_at) VALUES (1, 'pre', 'admin', 1)`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	u, err := s.GetUserByTelegramID(1)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.MaxDevices != nil {
		t.Fatalf("pre-caps row must read back as nil (global default), got %v", *u.MaxDevices)
	}
	if u.ExpiresAt != nil {
		t.Fatalf("expiry column must survive the max_devices migration: %+v", u.ExpiresAt)
	}

	cap := int64(7)
	if err := s.SetUserMaxDevices(u.ID, &cap); err != nil {
		t.Fatalf("set cap: %v", err)
	}
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { s2.Close() })
	u2, err := s2.GetUserByTelegramID(1)
	if err != nil || u2.MaxDevices == nil || *u2.MaxDevices != 7 {
		t.Fatalf("cap lost across reopen: %+v err=%v", u2, err)
	}
}

// TestConfigFetchedAtMigration verifies the devices.config_fetched_at
// migration with the same raw-old-schema pattern.
func TestConfigFetchedAtMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-fetched.db")
	oldSchema := `
	CREATE TABLE IF NOT EXISTS users (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id INTEGER UNIQUE,
		username    TEXT    NOT NULL DEFAULT '',
		role        TEXT    NOT NULL DEFAULT 'user',
		disabled    INTEGER NOT NULL DEFAULT 0,
		created_at  INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS devices (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name           TEXT    NOT NULL,
		protocol       TEXT    NOT NULL DEFAULT 'wg',
		public_key     TEXT    NOT NULL UNIQUE,
		private_key    TEXT    NOT NULL,
		preshared_key  TEXT    NOT NULL DEFAULT '',
		ipv4           TEXT    NOT NULL UNIQUE,
		enabled        INTEGER NOT NULL DEFAULT 1,
		created_at     INTEGER NOT NULL,
		last_handshake INTEGER NOT NULL DEFAULT 0,
		last_rx        INTEGER NOT NULL DEFAULT 0,
		last_tx        INTEGER NOT NULL DEFAULT 0,
		total_rx       INTEGER NOT NULL DEFAULT 0,
		total_tx       INTEGER NOT NULL DEFAULT 0
	);`
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("old schema: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO users (username, created_at) VALUES ('old', 1)`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO devices (user_id, name, public_key, private_key, ipv4, created_at) VALUES (1, 'legacy', 'oldpub', 'oldpriv', '10.8.0.2', 1)`); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	d, err := s.GetDeviceByKey("oldpub")
	if err != nil {
		t.Fatalf("get legacy device: %v", err)
	}
	if d.ConfigFetchedAt != nil {
		t.Fatalf("legacy device must read back as never fetched, got %+v", d.ConfigFetchedAt)
	}
	now := time.Now()
	if err := s.TouchDeviceConfigFetched(d.ID, now); err != nil {
		t.Fatalf("touch: %v", err)
	}
	d, _ = s.GetDeviceByKey("oldpub")
	if d.ConfigFetchedAt == nil || d.ConfigFetchedAt.Unix() < now.Unix()-2 {
		t.Fatalf("fetched stamp not persisted: %+v", d.ConfigFetchedAt)
	}

	// Touching a missing device is an error, not a silent no-op.
	if err := s.TouchDeviceConfigFetched(9999, now); err != ErrNotFound {
		t.Fatalf("touch missing device: %v", err)
	}
}
