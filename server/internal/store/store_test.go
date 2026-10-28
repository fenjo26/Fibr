package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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

// TestEffectiveExpiry covers the pure deadline helpers: the effective
// deadline is the earlier of the account's own and its owner's, NULL means
// "no deadline" and is ignored, and only a passed deadline counts as expired.
func TestEffectiveExpiry(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-24 * time.Hour)
	later := now.Add(24 * time.Hour)

	cases := []struct {
		own, owner *time.Time
		want       *time.Time // nil = unlimited
		expired    bool
	}{
		{own: nil, owner: nil, want: nil, expired: false},             // unlimited everywhere
		{own: nil, owner: &later, want: &later, expired: false},       // admin's deadline governs
		{own: nil, owner: &earlier, want: &earlier, expired: true},    // unpaid admin kills the branch
		{own: &later, owner: nil, want: &later, expired: false},       // own deadline only
		{own: &earlier, owner: nil, want: &earlier, expired: true},    // own deadline passed
		{own: &earlier, owner: &later, want: &earlier, expired: true}, // own is earlier and passed
		{own: &later, owner: &earlier, want: &earlier, expired: true}, // owner is earlier and passed
		{own: &later, owner: &later, want: &later, expired: false},    // both fine
	}
	for i, c := range cases {
		got := EffectiveExpiry(c.own, c.owner)
		if (got == nil) != (c.want == nil) {
			t.Fatalf("case %d: EffectiveExpiry = %v, want %v", i, got, c.want)
		}
		if got != nil && !got.Equal(*c.want) {
			t.Fatalf("case %d: EffectiveExpiry = %v, want %v", i, got, c.want)
		}
		if exp := ExpiredAt(got, now); exp != c.expired {
			t.Fatalf("case %d: ExpiredAt = %v, want %v", i, exp, c.expired)
		}
	}
	if ExpiredAt(nil, now) {
		t.Fatal("nil deadline must never expire")
	}
}

// TestUserExpiry covers the manual-monetization store surface (see
// docs/billing.md): SetUserExpiry round-trip, ListActiveDevices' filtering
// (enabled devices of unexpired accounts, owner cascade) and
// ListExpiredUserIDs.
func TestUserExpiry(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	earlier := now.Add(-time.Hour)
	later := now.Add(24 * time.Hour)
	unix := func(ts time.Time) *int64 { v := ts.Unix(); return &v }

	mkUser := func(u *User) *User {
		if err := s.CreateUser(u); err != nil {
			t.Fatalf("seed user %s: %v", u.Username, err)
		}
		return u
	}
	nextIP := 2
	mkDev := func(uid int64, name string, enabled bool) *Device {
		d := &Device{UserID: uid, Name: name, PublicKey: "pub-" + name, PrivateKey: "priv-" + name,
			IPv4: "10.8.0." + strconv.Itoa(nextIP), Enabled: enabled}
		nextIP++
		if err := s.CreateDevice(d); err != nil {
			t.Fatalf("seed device %s: %v", name, err)
		}
		return d
	}

	// The cast: a paid admin, an expired admin, and users in every expiry
	// combination around them.
	adminOK := mkUser(&User{Username: "admin-ok", Role: RoleAdmin})
	adminExp := mkUser(&User{Username: "admin-expired", Role: RoleAdmin})
	if err := s.SetUserExpiry(adminExp.ID, unix(earlier)); err != nil {
		t.Fatalf("set admin expiry: %v", err)
	}

	plain := mkUser(&User{Username: "plain"})              // no owner, no deadline
	expiredUser := mkUser(&User{Username: "expired-user"}) // own deadline passed
	if err := s.SetUserExpiry(expiredUser.ID, unix(earlier)); err != nil {
		t.Fatalf("set user expiry: %v", err)
	}
	underOK := mkUser(&User{Username: "under-ok", OwnerID: &adminOK.ID})        // NULL deadline under a paid admin
	underExp := mkUser(&User{Username: "under-expired", OwnerID: &adminExp.ID}) // NULL deadline under an expired admin
	lateOwn := mkUser(&User{Username: "late-own", OwnerID: &adminExp.ID})       // own future deadline, owner's past one wins
	if err := s.SetUserExpiry(lateOwn.ID, unix(later)); err != nil {
		t.Fatalf("set late-own expiry: %v", err)
	}

	devPlain := mkDev(plain.ID, "plain-phone", true)
	devExpired := mkDev(expiredUser.ID, "expired-phone", true)
	devUnderOK := mkDev(underOK.ID, "under-ok-phone", true)
	devUnderExp := mkDev(underExp.ID, "under-expired-phone", true)
	devLateOwn := mkDev(lateOwn.ID, "late-own-phone", true)
	mkDev(plain.ID, "plain-phone-disabled", false) // enabled=false drops even a paid user's device

	// Active devices: only the ones whose whole chain is still paid for.
	active, err := s.ListActiveDevices(now)
	if err != nil {
		t.Fatalf("ListActiveDevices: %v", err)
	}
	got := map[int64]bool{}
	for _, d := range active {
		got[d.ID] = true
	}
	for _, d := range []*Device{devPlain, devUnderOK} {
		if !got[d.ID] {
			t.Fatalf("device %s (id %d) must be active, got %v", d.Name, d.ID, got)
		}
	}
	for _, d := range []*Device{devExpired, devUnderExp, devLateOwn} {
		if got[d.ID] {
			t.Fatalf("device %s (id %d) must be dropped by expiry, got %v", d.Name, d.ID, got)
		}
	}
	if len(active) != 2 {
		t.Fatalf("expected exactly 2 active devices, got %d: %v", len(active), active)
	}

	// The expired-id set is exactly the accounts behind those drops.
	ids, err := s.ListExpiredUserIDs(now)
	if err != nil {
		t.Fatalf("ListExpiredUserIDs: %v", err)
	}
	wantIDs := map[int64]bool{adminExp.ID: true, expiredUser.ID: true, underExp.ID: true, lateOwn.ID: true}
	if len(ids) != len(wantIDs) {
		t.Fatalf("expired ids: got %v want %v", ids, wantIDs)
	}
	for _, id := range ids {
		if !wantIDs[id] {
			t.Fatalf("unexpected expired id %d (want %v)", id, wantIDs)
		}
	}

	// SetUserExpiry round-trip: renewing the user brings the device back,
	// clearing the deadline (NULL) keeps it active.
	if err := s.SetUserExpiry(expiredUser.ID, unix(later)); err != nil {
		t.Fatalf("renew: %v", err)
	}
	gotUser, err := s.GetUser(expiredUser.ID)
	if err != nil || gotUser.ExpiresAt == nil || !gotUser.ExpiresAt.Equal(later.Truncate(time.Second)) {
		t.Fatalf("renewed expiry not persisted: %+v err=%v", gotUser, err)
	}
	if active, _ = s.ListActiveDevices(now); len(active) != 3 {
		t.Fatalf("renewal must restore the device, got %d active", len(active))
	}
	if err := s.SetUserExpiry(expiredUser.ID, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if gotUser, _ = s.GetUser(expiredUser.ID); gotUser.ExpiresAt != nil {
		t.Fatalf("nil must clear the deadline (unlimited), got %+v", gotUser)
	}
}

// TestExpiryMigration verifies the pragma-gated users.expires_at migration:
// a database from before the billing feature (roles schema, no expires_at
// column) must be upgraded in place on the next Open, existing rows reading
// back as NULL (unlimited), and repeated Opens must be a no-op that keeps
// values written after the migration.
func TestExpiryMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-billing.db")
	oldSchema := `
	CREATE TABLE IF NOT EXISTS users (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id INTEGER UNIQUE,
		username    TEXT    NOT NULL DEFAULT '',
		role        TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin','superadmin')),
		owner_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
		disabled    INTEGER NOT NULL DEFAULT 0,
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
	if u.ExpiresAt != nil {
		t.Fatalf("pre-billing row must read back as unlimited, got %+v", u.ExpiresAt)
	}

	// Deadlines written after the migration survive a reopen (no-op pass).
	exp := time.Now().Add(48 * time.Hour).Unix()
	if err := s.SetUserExpiry(u.ID, &exp); err != nil {
		t.Fatalf("set expiry: %v", err)
	}
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { s2.Close() })
	u2, err := s2.GetUserByTelegramID(1)
	if err != nil || u2.ExpiresAt == nil {
		t.Fatalf("expiry lost across reopen: %+v err=%v", u2, err)
	}
}

// TestAccessTokenMigration verifies the pragma-gated users.access_token_hash
// migration (app access keys): a database from before the feature must be
// upgraded in place on the next Open, and repeated Opens must be a no-op
// that keeps keys written after the migration.
func TestAccessTokenMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pre-access-keys.db")
	// The referral-era users schema: every column except access_token_hash.
	oldSchema := `
	CREATE TABLE IF NOT EXISTS users (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		telegram_id INTEGER UNIQUE,
		username    TEXT    NOT NULL DEFAULT '',
		role        TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin','superadmin')),
		owner_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
		disabled    INTEGER NOT NULL DEFAULT 0,
		expires_at  INTEGER,
		max_devices INTEGER,
		referral_code TEXT,
		referred_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
		created_at  INTEGER NOT NULL
	);`
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("old schema: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO users (telegram_id, username, role, created_at) VALUES (1, 'pre', 'user', 1)`); err != nil {
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
	// Pre-feature row: no key, and lookups by hash miss cleanly.
	if has, err := s.HasUserAccessToken(u.ID); err != nil || has {
		t.Fatalf("pre-feature row must have no key, got has=%v err=%v", has, err)
	}
	if _, err := s.GetUserByAccessTokenHash("deadbeef"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Keys written after the migration survive a reopen (no-op pass).
	h := sha256Hex("first-key")
	if err := s.SetUserAccessToken(u.ID, &h); err != nil {
		t.Fatalf("set key: %v", err)
	}
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	t.Cleanup(func() { s2.Close() })
	got, err := s2.GetUserByAccessTokenHash(h)
	if err != nil || got.ID != u.ID {
		t.Fatalf("key lost across reopen: %+v err=%v", got, err)
	}
}

// TestAccessTokenRotationAndRevoke covers the app-access-key store surface:
// hash round-trip, rotation (a second Set replaces the first key), revoke
// (NULL clears it) and the shared-key uniqueness guard.
func TestAccessTokenRotationAndRevoke(t *testing.T) {
	s := openTestStore(t)

	alice := &User{Username: "alice"}
	bob := &User{Username: "bob"}
	if err := s.CreateUser(alice); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if err := s.CreateUser(bob); err != nil {
		t.Fatalf("create bob: %v", err)
	}

	first := sha256Hex("key-one")
	if err := s.SetUserAccessToken(alice.ID, &first); err != nil {
		t.Fatalf("set first key: %v", err)
	}
	if has, _ := s.HasUserAccessToken(alice.ID); !has {
		t.Fatal("HasUserAccessToken must be true after set")
	}
	got, err := s.GetUserByAccessTokenHash(first)
	if err != nil || got.ID != alice.ID {
		t.Fatalf("lookup by hash: %+v err=%v", got, err)
	}

	// Rotation: the second key replaces the first for the same account.
	second := sha256Hex("key-two")
	if err := s.SetUserAccessToken(alice.ID, &second); err != nil {
		t.Fatalf("rotate key: %v", err)
	}
	if _, err := s.GetUserByAccessTokenHash(first); err != ErrNotFound {
		t.Fatalf("old key must stop resolving after rotation, got %v", err)
	}
	got, err = s.GetUserByAccessTokenHash(second)
	if err != nil || got.ID != alice.ID {
		t.Fatalf("new key must resolve: %+v err=%v", got, err)
	}

	// Uniqueness: two accounts may not share one key (partial unique index).
	if err := s.SetUserAccessToken(bob.ID, &second); err == nil {
		t.Fatal("expected UNIQUE violation when sharing a key between accounts")
	}

	// Revoke: NULL clears the key; HasUserAccessToken flips back to false.
	if err := s.SetUserAccessToken(alice.ID, nil); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.GetUserByAccessTokenHash(second); err != ErrNotFound {
		t.Fatalf("revoked key must stop resolving, got %v", err)
	}
	if has, _ := s.HasUserAccessToken(alice.ID); has {
		t.Fatal("HasUserAccessToken must be false after revoke")
	}

	// Setting a key on a missing account is ErrNotFound, not a silent no-op.
	ghost := sha256Hex("ghost")
	if err := s.SetUserAccessToken(99999, &ghost); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for missing user, got %v", err)
	}
}

// sha256Hex mirrors the API layer's storage form (access_key.go hashAccessKey)
// so tests exercise real hash values, not placeholders.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
