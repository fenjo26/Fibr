// Package store persists users, devices and server settings in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrInviteUsed is returned by MarkUsed when the invite exists but has
// already been consumed (one-time use).
var ErrInviteUsed = errors.New("invite already used")

// Role values for User.Role.
const (
	RoleUser       = "user"
	RoleAdmin      = "admin"
	RoleSuperadmin = "superadmin"
)

// User is a Fibr account. Telegram-linked users are created lazily on first
// Mini App open or /start; admins can also create manual (panel-only) users.
// OwnerID records who created the account (an admin's or the superadmin's
// user id); it is nil for self-registered and env-promoted accounts.
type User struct {
	ID         int64     `json:"id"`
	TelegramID *int64    `json:"telegram_id"`
	Username   string    `json:"username"`
	Role       string    `json:"role"` // "user" | "admin" | "superadmin"
	OwnerID    *int64    `json:"owner_id"`
	Disabled   bool      `json:"disabled"`
	CreatedAt  time.Time `json:"created_at"`
}

// Protocol values for Device.Protocol.
const (
	// ProtocolWG is the classic WireGuard device: PublicKey/PrivateKey/
	// PresharedKey hold wg keys, IPv4 is the address inside the tunnel.
	ProtocolWG = "wg"
	// ProtocolXray is a VLESS+Reality client. There are no wg keys; instead:
	//   PublicKey    = the client UUID (what a vless:// link carries and the
	//                  xray inbound lists under clients[].id),
	//   PrivateKey   = "" (empty),
	//   PresharedKey = "" (empty).
	// IPv4 is still allocated (the column is UNIQUE NOT NULL) but is unused
	// by the protocol. No per-device stats exist for xray in this MVP.
	ProtocolXray = "xray"
)

// Device is one VPN credential owned by a user: a WireGuard peer (Protocol
// "wg") or an Xray/VLESS client (Protocol "xray"). The private key is
// generated server-side so the panel can render full client configs and QR
// codes (the same trade-off wg-easy makes); see README for the security
// implications.
type Device struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	Name          string    `json:"name"`
	Protocol      string    `json:"protocol"` // "wg" | "xray" (never empty after CreateDevice)
	PublicKey     string    `json:"public_key"`
	PrivateKey    string    `json:"-"`
	PresharedKey  string    `json:"-"`
	IPv4          string    `json:"ipv4"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	LastHandshake time.Time `json:"last_handshake"` // zero = never
	LastRx        int64     `json:"-"`
	LastTx        int64     `json:"-"`
	TotalRx       int64     `json:"total_rx"`
	TotalTx       int64     `json:"total_tx"`
}

// Invite is a one-time self-enrollment code (stage 4). The admin generates
// it in the panel; a new user presents it on the first Mini App sign-in.
// UsedBy/UsedAt are nil until the code is consumed by registration.
type Invite struct {
	Code      string     `json:"code"`
	Note      string     `json:"note"`
	CreatedBy int64      `json:"created_by"` // 0 = created with the static admin token
	CreatedAt time.Time  `json:"created_at"`
	UsedBy    *int64     `json:"used_by"`
	UsedAt    *time.Time `json:"used_at"`
}

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	telegram_id INTEGER UNIQUE,
	username    TEXT    NOT NULL DEFAULT '',
	role        TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin','superadmin')),
	owner_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
	disabled    INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS devices (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name           TEXT    NOT NULL,
	protocol       TEXT    NOT NULL DEFAULT 'wg' CHECK (protocol IN ('wg','xray')),
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
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS invites (
	code       TEXT PRIMARY KEY,
	note       TEXT    NOT NULL DEFAULT '',
	created_by INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	used_by    INTEGER REFERENCES users(id),
	used_at    INTEGER
);
`

// Open opens (or creates) the database at path and applies migrations.
//
// Migrations are idempotent: Open() runs on every start, so each step checks
// the actual database state before altering anything (schema deltas are gated
// on pragma table_info, never on schema version numbers).
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite writes are serialized; keeps things simple and safe
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := migrateUsersRoles(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate users roles: %w", err)
	}
	if err := migrateDevicesProtocol(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate devices.protocol: %w", err)
	}
	return &Store{db: db}, nil
}

// migrateUsersRoles upgrades pre-reseller databases to the three-role model:
// users.role gains the 'superadmin' value and users.owner_id is added
// (who created the account). SQLite cannot ALTER a CHECK constraint on a
// live table, so the users table is rebuilt:
//
//	PRAGMA foreign_keys=off (renames/drops would otherwise trip the
//	devices.user_id and invites.used_by foreign keys)
//	CREATE TABLE users_new (new schema)
//	INSERT INTO users_new SELECT ... FROM users (ids preserved, owner NULL)
//	DROP TABLE users
//	ALTER TABLE users_new RENAME TO users
//
// The whole rebuild runs in one transaction; FK enforcement is restored
// afterwards and verified with PRAGMA foreign_key_check. Idempotent: the gate
// checks that users already has the owner_id column and the new CHECK
// (via sqlite_master), so fresh databases and repeated Open() calls no-op.
func migrateUsersRoles(db *sql.DB) error {
	rows, err := db.Query(`pragma table_info(users)`)
	if err != nil {
		return err
	}
	hasOwner := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "owner_id" {
			hasOwner = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	hasSuperCheck := false
	if hasOwner {
		var schemaSQL string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'users'`).Scan(&schemaSQL); err != nil {
			return err
		}
		hasSuperCheck = strings.Contains(schemaSQL, "'superadmin'")
		if hasSuperCheck {
			return nil // already migrated (normal case on every start)
		}
	}
	// Old (or hand-mangled) users table: rebuild it.

	// FK enforcement must be off while the referenced table is swapped; the
	// pragma is a no-op inside a transaction, so it runs before BEGIN.
	if _, err := db.Exec(`pragma foreign_keys = off`); err != nil {
		return err
	}
	defer func() {
		_, _ = db.Exec(`pragma foreign_keys = on`)
	}()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{
		`CREATE TABLE users_new (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_id INTEGER UNIQUE,
			username    TEXT    NOT NULL DEFAULT '',
			role        TEXT    NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin','superadmin')),
			owner_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
			disabled    INTEGER NOT NULL DEFAULT 0,
			created_at  INTEGER NOT NULL
		)`,
		`INSERT INTO users_new (id, telegram_id, username, role, owner_id, disabled, created_at)
		 SELECT id, telegram_id, username, role, NULL, disabled, created_at FROM users`,
		`DROP TABLE users`,
		`ALTER TABLE users_new RENAME TO users`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("%q: %w", q[:32], err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Restore FK enforcement and prove nothing was orphaned by the swap.
	if _, err := db.Exec(`pragma foreign_keys = on`); err != nil {
		return err
	}
	chk, err := db.Query(`pragma foreign_key_check`)
	if err != nil {
		return err
	}
	defer chk.Close()
	if chk.Next() {
		return fmt.Errorf("foreign key check failed after users rebuild")
	}
	return chk.Err()
}

// migrateDevicesProtocol adds the devices.protocol column ("wg"|"xray") to
// databases created before the Xray integration. CREATE TABLE IF NOT EXISTS
// above only covers fresh databases; for existing ones we check pragma
// table_info and ALTER TABLE when the column is missing. Existing rows get
// the column default 'wg', which is correct: before Xray every device was a
// WireGuard peer.
func migrateDevicesProtocol(db *sql.DB) error {
	rows, err := db.Query(`pragma table_info(devices)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasProtocol := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == "protocol" {
			hasProtocol = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if hasProtocol {
		return nil // already migrated (normal case on every start)
	}
	_, err = db.Exec(`ALTER TABLE devices ADD COLUMN protocol TEXT NOT NULL DEFAULT 'wg' CHECK (protocol IN ('wg','xray'))`)
	return err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

/* ---------------------------- users ---------------------------- */

const userCols = `id, telegram_id, username, role, owner_id, disabled, created_at`

// CreateUser inserts a user; ID and CreatedAt are filled in. OwnerID (if
// set) records who created the account.
func (s *Store) CreateUser(u *User) error {
	if u.Role == "" {
		u.Role = RoleUser
	}
	now := time.Now().Unix()
	var tid, oid any
	if u.TelegramID != nil {
		tid = *u.TelegramID
	}
	if u.OwnerID != nil {
		oid = *u.OwnerID
	}
	res, err := s.db.Exec(
		`INSERT INTO users (telegram_id, username, role, owner_id, disabled, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		tid, u.Username, u.Role, oid, b2i(u.Disabled), now,
	)
	if err != nil {
		return err
	}
	u.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	u.CreatedAt = time.Unix(now, 0)
	return nil
}

// GetUser returns a user by id.
func (s *Store) GetUser(id int64) (*User, error) {
	row := s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetUserByTelegramID returns a user by telegram id, or ErrNotFound.
func (s *Store) GetUserByTelegramID(tid int64) (*User, error) {
	row := s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE telegram_id = ?`, tid)
	return scanUser(row)
}

// ListUsers returns all users ordered by id. Scoping by role/ownership is
// done by the API layer.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// ListUserIDsByOwner returns the ids of all accounts created by owner (the
// admin's reseller subtree, one level: admins can only create plain users).
func (s *Store) ListUserIDsByOwner(owner int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM users WHERE owner_id = ? ORDER BY id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetUserRole sets the role ("user", "admin" or "superadmin").
func (s *Store) SetUserRole(id int64, role string) error {
	res, err := s.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	return touched(res, err)
}

// SetUserOwner records who created (or now owns) the account; owner 0 clears
// the link (owner_id -> NULL).
func (s *Store) SetUserOwner(id, owner int64) error {
	var oid any
	if owner > 0 {
		oid = owner
	}
	res, err := s.db.Exec(`UPDATE users SET owner_id = ? WHERE id = ?`, oid, id)
	return touched(res, err)
}

// SetUserDisabled enables or disables a user (disabled users cannot sign in;
// revoking their peers is done by disabling/deleting their devices).
func (s *Store) SetUserDisabled(id int64, disabled bool) error {
	res, err := s.db.Exec(`UPDATE users SET disabled = ? WHERE id = ?`, b2i(disabled), id)
	return touched(res, err)
}

// SetUsername updates the display username.
func (s *Store) SetUsername(id int64, username string) error {
	_, err := s.db.Exec(`UPDATE users SET username = ? WHERE id = ?`, username, id)
	return err
}

// DeleteUser removes a user; their devices are removed by cascade. The caller
// is responsible for syncing WireGuard peers afterwards. Invites the user
// consumed keep their "used" state but lose the reference (invites.used_by
// has no ON DELETE clause), so deletion never trips the foreign key.
func (s *Store) DeleteUser(id int64) error {
	if _, err := s.db.Exec(`UPDATE invites SET used_by = NULL WHERE used_by = ?`, id); err != nil {
		return err
	}
	res, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return touched(res, err)
}

func scanUser(r interface{ Scan(...any) error }) (*User, error) {
	var u User
	var tid, oid sql.NullInt64
	var disabled int
	var created int64
	if err := r.Scan(&u.ID, &tid, &u.Username, &u.Role, &oid, &disabled, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if tid.Valid {
		t := tid.Int64
		u.TelegramID = &t
	}
	if oid.Valid {
		o := oid.Int64
		u.OwnerID = &o
	}
	u.Disabled = disabled != 0
	u.CreatedAt = time.Unix(created, 0)
	return &u, nil
}

/* ---------------------------- devices ---------------------------- */

// CreateDevice inserts a device; CreatedAt is filled in. Protocol defaults
// to "wg" when empty. See the Device struct for the key-field conventions of
// each protocol.
func (s *Store) CreateDevice(d *Device) error {
	if d.Protocol == "" {
		d.Protocol = ProtocolWG
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO devices (user_id, name, protocol, public_key, private_key, preshared_key, ipv4, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.UserID, d.Name, d.Protocol, d.PublicKey, d.PrivateKey, d.PresharedKey, d.IPv4, b2i(d.Enabled), now,
	)
	if err != nil {
		return err
	}
	d.ID, err = res.LastInsertId()
	if err != nil {
		return err
	}
	d.CreatedAt = time.Unix(now, 0)
	return nil
}

const deviceCols = `id, user_id, name, protocol, public_key, private_key, preshared_key, ipv4, enabled, created_at, last_handshake, last_rx, last_tx, total_rx, total_tx`

// GetDevice returns a device by id.
func (s *Store) GetDevice(id int64) (*Device, error) {
	row := s.db.QueryRow(`SELECT `+deviceCols+` FROM devices WHERE id = ?`, id)
	return scanDevice(row)
}

// GetDeviceByKey returns a device by WireGuard public key.
func (s *Store) GetDeviceByKey(publicKey string) (*Device, error) {
	row := s.db.QueryRow(`SELECT `+deviceCols+` FROM devices WHERE public_key = ?`, publicKey)
	return scanDevice(row)
}

// ListDevices returns devices; userID 0 means "all devices".
func (s *Store) ListDevices(userID int64) ([]Device, error) {
	q := `SELECT ` + deviceCols + ` FROM devices`
	var args []any
	if userID > 0 {
		q += ` WHERE user_id = ?`
		args = append(args, userID)
	}
	q += ` ORDER BY id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// SetDeviceEnabled toggles a peer. Disabled peers are excluded from the
// WireGuard config sync, which effectively cuts them off.
func (s *Store) SetDeviceEnabled(id int64, enabled bool) error {
	res, err := s.db.Exec(`UPDATE devices SET enabled = ? WHERE id = ?`, b2i(enabled), id)
	return touched(res, err)
}

// DeleteDevice removes a device.
func (s *Store) DeleteDevice(id int64) error {
	res, err := s.db.Exec(`DELETE FROM devices WHERE id = ?`, id)
	return touched(res, err)
}

// UpdateDeviceStats stores fresh peer counters. WireGuard resets its transfer
// counters when the interface restarts, so callers pass raw values and the
// totals accumulated here must already account for counter resets.
func (s *Store) UpdateDeviceStats(id int64, lastHandshake time.Time, lastRx, lastTx, totalRx, totalTx int64) error {
	hs := int64(0)
	if !lastHandshake.IsZero() {
		hs = lastHandshake.Unix()
	}
	_, err := s.db.Exec(
		`UPDATE devices SET last_handshake = ?, last_rx = ?, last_tx = ?, total_rx = ?, total_tx = ? WHERE id = ?`,
		hs, lastRx, lastTx, totalRx, totalTx, id,
	)
	return err
}

func scanDevice(r interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	var enabled int
	var created, hs int64
	if err := r.Scan(&d.ID, &d.UserID, &d.Name, &d.Protocol, &d.PublicKey, &d.PrivateKey, &d.PresharedKey, &d.IPv4,
		&enabled, &created, &hs, &d.LastRx, &d.LastTx, &d.TotalRx, &d.TotalTx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if d.Protocol == "" {
		d.Protocol = ProtocolWG // pre-migration rows; defensive
	}
	d.Enabled = enabled != 0
	d.CreatedAt = time.Unix(created, 0)
	if hs > 0 {
		d.LastHandshake = time.Unix(hs, 0)
	}
	return &d, nil
}

/* ---------------------------- invites ---------------------------- */

// CreateInvite inserts an invite; the code is chosen by the caller (the API
// generates a random one). CreatedAt is filled in.
func (s *Store) CreateInvite(inv *Invite) error {
	now := time.Now().Unix()
	if _, err := s.db.Exec(
		`INSERT INTO invites (code, note, created_by, created_at) VALUES (?, ?, ?, ?)`,
		inv.Code, inv.Note, inv.CreatedBy, now,
	); err != nil {
		return err
	}
	inv.CreatedAt = time.Unix(now, 0)
	return nil
}

// GetInvite returns an invite by code, or ErrNotFound.
func (s *Store) GetInvite(code string) (*Invite, error) {
	row := s.db.QueryRow(`SELECT code, note, created_by, created_at, used_by, used_at FROM invites WHERE code = ?`, code)
	return scanInvite(row)
}

// ListInvites returns all invites, newest first.
func (s *Store) ListInvites() ([]Invite, error) {
	rows, err := s.db.Query(`SELECT code, note, created_by, created_at, used_by, used_at FROM invites ORDER BY created_at DESC, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

// MarkUsed atomically consumes an invite: the UPDATE only matches rows with
// used_by IS NULL, so exactly one concurrent registration can win. Returns
// ErrInviteUsed when the code was already consumed, ErrNotFound when the code
// does not exist at all.
func (s *Store) MarkUsed(code string, userID int64) error {
	now := time.Now().Unix()
	res, err := s.db.Exec(`UPDATE invites SET used_by = ?, used_at = ? WHERE code = ? AND used_by IS NULL`, userID, now, code)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetInvite(code); err != nil {
			return err // ErrNotFound (or a storage error)
		}
		return ErrInviteUsed
	}
	return nil
}

// DeleteInvite removes an invite (used or not). Deleting a used invite is
// allowed — the account it created stays.
func (s *Store) DeleteInvite(code string) error {
	res, err := s.db.Exec(`DELETE FROM invites WHERE code = ?`, code)
	return touched(res, err)
}

func scanInvite(r interface{ Scan(...any) error }) (*Invite, error) {
	var inv Invite
	var usedBy, usedAt sql.NullInt64
	var created int64
	if err := r.Scan(&inv.Code, &inv.Note, &inv.CreatedBy, &created, &usedBy, &usedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	inv.CreatedAt = time.Unix(created, 0)
	if usedBy.Valid {
		uid := usedBy.Int64
		inv.UsedBy = &uid
	}
	if usedAt.Valid && usedAt.Int64 > 0 {
		t := time.Unix(usedAt.Int64, 0)
		inv.UsedAt = &t
	}
	return &inv, nil
}

/* ---------------------------- settings ---------------------------- */

// GetSetting returns a settings value or ErrNotFound.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SetSetting upserts a settings value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

/* ---------------------------- helpers ---------------------------- */

// NextIPv4 returns the first free host address in subnet (server takes .1).
func (s *Store) NextIPv4(subnet string) (string, error) {
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", err
	}
	base := ipnet.IP.To4()
	if base == nil {
		return "", fmt.Errorf("subnet %s is not IPv4", subnet)
	}
	rows, err := s.db.Query(`SELECT ipv4 FROM devices`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	used := map[int]bool{1: true} // .1 is the server
	for rows.Next() {
		var ipStr string
		if err := rows.Scan(&ipStr); err != nil {
			return "", err
		}
		if ip := net.ParseIP(ipStr).To4(); ip != nil && ipnet.Contains(ip) {
			used[int(ip[3])] = true
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	for i := 2; i <= 254; i++ {
		if !used[i] {
			out := make(net.IP, 4)
			copy(out, base)
			out[3] = byte(i)
			return out.String(), nil
		}
	}
	return "", fmt.Errorf("subnet %s is exhausted", subnet)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func touched(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
