package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUserCRUD(t *testing.T) {
	s := openTestStore(t)

	tid := int64(100500)
	u := &User{TelegramID: &tid, Username: "ivan", Role: "user"}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.ID == 0 || u.Role != "user" {
		t.Fatalf("unexpected user after create: %+v", u)
	}

	got, err := s.GetUserByTelegramID(100500)
	if err != nil {
		t.Fatalf("get by telegram id: %v", err)
	}
	if got.Username != "ivan" {
		t.Fatalf("username mismatch: %s", got.Username)
	}

	if err := s.SetUserRole(got.ID, "admin"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	got, _ = s.GetUser(got.ID)
	if got.Role != "admin" {
		t.Fatalf("role not updated: %s", got.Role)
	}

	if err := s.SetUserDisabled(got.ID, true); err != nil {
		t.Fatalf("set disabled: %v", err)
	}
	got, _ = s.GetUser(got.ID)
	if !got.Disabled {
		t.Fatal("disabled flag not persisted")
	}

	if err := s.DeleteUser(got.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetUserByTelegramID(100500); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDevicesAndIPv4Allocation(t *testing.T) {
	s := openTestStore(t)
	u := &User{Username: "alice"}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	mk := func(name string) *Device {
		ip, err := s.NextIPv4("10.8.0.0/24")
		if err != nil {
			t.Fatalf("next ip: %v", err)
		}
		d := &Device{UserID: u.ID, Name: name, PublicKey: "pub-" + name, PrivateKey: "priv", PresharedKey: "psk", IPv4: ip, Enabled: true}
		if err := s.CreateDevice(d); err != nil {
			t.Fatalf("create device: %v", err)
		}
		return d
	}

	d1 := mk("phone")  // 10.8.0.2
	d2 := mk("laptop") // 10.8.0.3
	if d1.IPv4 != "10.8.0.2" || d2.IPv4 != "10.8.0.3" {
		t.Fatalf("allocation order: %s then %s", d1.IPv4, d2.IPv4)
	}

	// Deleting the first device must free its address for reuse.
	if err := s.DeleteDevice(d1.ID); err != nil {
		t.Fatalf("delete device: %v", err)
	}
	d3 := mk("tablet")
	if d3.IPv4 != "10.8.0.2" {
		t.Fatalf("expected freed address reuse, got %s", d3.IPv4)
	}

	// Stats update round-trip.
	if err := s.UpdateDeviceStats(d3.ID, d3.CreatedAt, 100, 200, 100, 200); err != nil {
		t.Fatalf("update stats: %v", err)
	}
	got, err := s.GetDeviceByKey(d3.PublicKey)
	if err != nil {
		t.Fatalf("get by key: %v", err)
	}
	if got.TotalRx != 100 || got.TotalTx != 200 || got.LastHandshake.IsZero() {
		t.Fatalf("stats mismatch: %+v", got)
	}

	// Cascade: deleting the user removes devices.
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := s.GetDevice(d3.ID); err != ErrNotFound {
		t.Fatalf("expected cascade delete, got %v", err)
	}
}

func TestSettings(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetSetting("k"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.SetSetting("k", "v1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetSetting("k", "v2"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	v, err := s.GetSetting("k")
	if err != nil || v != "v2" {
		t.Fatalf("get: %q err=%v", v, err)
	}
}

// TestProtocolMigration verifies the idempotent devices.protocol migration:
// a database created with the pre-Xray schema (no protocol column) must be
// upgraded in place on the next Open, existing rows reading back as "wg",
// while repeated Open() calls on the new schema must be a no-op.
func TestProtocolMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// Build a database with the OLD devices schema (no protocol column).
	oldSchema := `
	CREATE TABLE IF NOT EXISTS users (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id INTEGER UNIQUE,
		username    TEXT    NOT NULL DEFAULT '',
		role        TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
		disabled    INTEGER NOT NULL DEFAULT 0,
		created_at  INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS devices (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name           TEXT    NOT NULL,
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
	if _, err := raw.Exec(`INSERT INTO users (telegram_id, username, role, created_at) VALUES (1, 'old', 'user', 1)`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO devices (user_id, name, public_key, private_key, ipv4, created_at) VALUES (1, 'legacy', 'oldpub', 'oldpriv', '10.8.0.2', 1)`); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	raw.Close()

	// Open through the store: the migration must add the column and backfill
	// existing rows with the default.
	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	got, err := s.GetDeviceByKey("oldpub")
	if err != nil {
		t.Fatalf("get legacy device: %v", err)
	}
	if got.Protocol != ProtocolWG {
		t.Fatalf("legacy device must default to wg, got %q", got.Protocol)
	}
	if got.PrivateKey != "oldpriv" || got.IPv4 != "10.8.0.2" {
		t.Fatalf("legacy columns damaged by migration: %+v", got)
	}

	// New-style rows round-trip both protocols.
	if err := s.CreateDevice(&Device{UserID: 1, Name: "xphone", Protocol: ProtocolXray, PublicKey: "uuid-1", IPv4: "10.8.0.3", Enabled: true}); err != nil {
		t.Fatalf("create xray device: %v", err)
	}
	x, err := s.GetDeviceByKey("uuid-1")
	if err != nil || x.Protocol != ProtocolXray {
		t.Fatalf("xray device round-trip: %+v err=%v", x, err)
	}
	if err := s.CreateDevice(&Device{UserID: 1, Name: "noprotocol", PublicKey: "pub-2", PrivateKey: "priv", IPv4: "10.8.0.4", Enabled: true}); err != nil {
		t.Fatalf("create default device: %v", err)
	}
	d, err := s.GetDeviceByKey("pub-2")
	if err != nil || d.Protocol != ProtocolWG {
		t.Fatalf("empty protocol must default to wg: %+v err=%v", d, err)
	}
	s.Close()

	// Re-opening a migrated database must be a harmless no-op.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { s2.Close() })
	if _, err := s2.GetDeviceByKey("oldpub"); err != nil {
		t.Fatalf("get after second open: %v", err)
	}
}

// TestUsersRolesMigration verifies the idempotent users rebuild migration:
// a database created with the pre-reseller schema (roles CHECK without
// 'superadmin', no owner_id column) must be upgraded in place on the next
// Open with all rows, ids and foreign keys (devices.user_id, invites.used_by)
// intact, while repeated Open() calls on the new schema must be a no-op.
func TestUsersRolesMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-users.db")

	// Build a database with the OLD users schema.
	oldSchema := `
	CREATE TABLE IF NOT EXISTS users (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id INTEGER UNIQUE,
		username    TEXT    NOT NULL DEFAULT '',
		role        TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
		disabled    INTEGER NOT NULL DEFAULT 0,
		created_at  INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS devices (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name           TEXT    NOT NULL,
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
	);
	CREATE TABLE IF NOT EXISTS invites (
		code       TEXT PRIMARY KEY,
		note       TEXT    NOT NULL DEFAULT '',
		created_by INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		used_by    INTEGER REFERENCES users(id),
		used_at    INTEGER
	);`
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("old schema: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO users (telegram_id, username, role, created_at) VALUES
		(1, 'owner-admin', 'admin', 1),
		(2, 'plain', 'user', 2),
		(3, 'second', 'user', 3)`); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO devices (user_id, name, public_key, private_key, ipv4, created_at) VALUES
		(2, 'phone', 'pub-1', 'priv', '10.8.0.2', 1)`); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	// used_by points at user 3, not user 2: invites.used_by has no ON DELETE
	// clause, so deleting its target is a (pre-existing) FK violation.
	if _, err := raw.Exec(`INSERT INTO invites (code, created_by, created_at, used_by, used_at) VALUES
		('abc123DEF9', 1, 1, 3, 2)`); err != nil {
		t.Fatalf("seed invite: %v", err)
	}
	raw.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	// Rows survived with ids, roles and NULL owner_id.
	admin, err := s.GetUserByTelegramID(1)
	if err != nil || admin.ID != 1 || admin.Role != "admin" || admin.OwnerID != nil {
		t.Fatalf("admin row damaged by migration: %+v err=%v", admin, err)
	}
	plain, err := s.GetUserByTelegramID(2)
	if err != nil || plain.Role != "user" || plain.OwnerID != nil {
		t.Fatalf("user row damaged by migration: %+v err=%v", plain, err)
	}

	// The new CHECK admits 'superadmin'; the old one would have rejected it.
	if err := s.SetUserRole(admin.ID, "superadmin"); err != nil {
		t.Fatalf("set superadmin role: %v", err)
	}
	got, _ := s.GetUser(admin.ID)
	if got.Role != "superadmin" {
		t.Fatalf("superadmin role not persisted: %s", got.Role)
	}

	// owner_id round-trip: link the two users, list the subtree, unlink.
	if err := s.SetUserOwner(plain.ID, admin.ID); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	got, _ = s.GetUser(plain.ID)
	if got.OwnerID == nil || *got.OwnerID != admin.ID {
		t.Fatalf("owner_id not persisted: %+v", got)
	}
	ids, err := s.ListUserIDsByOwner(admin.ID)
	if err != nil || len(ids) != 1 || ids[0] != plain.ID {
		t.Fatalf("ListUserIDsByOwner: %v err=%v", ids, err)
	}
	if err := s.SetUserOwner(plain.ID, 0); err != nil {
		t.Fatalf("clear owner: %v", err)
	}
	got, _ = s.GetUser(plain.ID)
	if got.OwnerID != nil {
		t.Fatalf("owner_id must be cleared with owner=0: %+v", got)
	}

	// Foreign keys still cascade after the rebuild.
	if err := s.SetUserOwner(plain.ID, admin.ID); err != nil {
		t.Fatalf("relink owner: %v", err)
	}
	if err := s.DeleteUser(plain.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := s.GetDeviceByKey("pub-1"); err != ErrNotFound {
		t.Fatalf("device cascade broken after migration: %v", err)
	}
	inv, err := s.GetInvite("abc123DEF9")
	if err != nil || inv.UsedBy == nil {
		t.Fatalf("invite row lost by migration: %+v err=%v", inv, err)
	}

	// AUTOINCREMENT continues past pre-migration ids (no id reuse).
	fresh := &User{Username: "fresh"}
	if err := s.CreateUser(fresh); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if fresh.ID <= 3 {
		t.Fatalf("AUTOINCREMENT not preserved: new user got id %d", fresh.ID)
	}

	// CreateUser stores owner_id on insert.
	owned := &User{Username: "owned", OwnerID: &fresh.ID}
	if err := s.CreateUser(owned); err != nil {
		t.Fatalf("create owned user: %v", err)
	}
	got, _ = s.GetUser(owned.ID)
	if got.OwnerID == nil || *got.OwnerID != fresh.ID {
		t.Fatalf("owner_id not stored on create: %+v", got)
	}
	s.Close()

	// Re-opening a migrated database must be a harmless no-op.
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { s2.Close() })
	if _, err := s2.GetUserByTelegramID(3); err != nil {
		t.Fatalf("get after second open: %v", err)
	}
}
