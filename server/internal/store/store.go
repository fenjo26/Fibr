// Package store persists users, devices and server settings in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrInviteUsed is returned by MarkUsed when the invite exists but has
// already been consumed (one-time use).
var ErrInviteUsed = errors.New("invite already used")

// ErrJoinCodeUsed is returned by ConsumeJoinCode when the code exists but
// was already exchanged for a config (or its TTL window has passed — both
// mean "ask the owner for a fresh code").
var ErrJoinCodeUsed = errors.New("join code already used or expired")

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
//
// ExpiresAt is the manual-monetization deadline (see docs/billing.md): nil
// means "no deadline". The deadline that actually governs the account is the
// earlier of its own ExpiresAt and its owner's — use EffectiveExpiry.
//
// MaxDevices caps how many devices the account may hold: nil = the global
// default (FIBR_MAX_DEVICES), 0 is a valid value meaning "no new devices".
// The cap that actually governs is the minimum of the account's own value and
// its owner's — use EffectiveMaxDevices.
//
// ReferralCode is the account's permanent referral code (see docs/
// referrals.md); ReferredBy records whose code the account signed up with
// (nil = organic). Unlike OwnerID (the reseller who created the account),
// ReferredBy is self-service attribution from a referral link.
type User struct {
	ID           int64      `json:"id"`
	TelegramID   *int64     `json:"telegram_id"`
	Username     string     `json:"username"`
	Role         string     `json:"role"` // "user" | "admin" | "superadmin"
	OwnerID      *int64     `json:"owner_id"`
	Disabled     bool       `json:"disabled"`
	ExpiresAt    *time.Time `json:"expires_at"`  // nil = unlimited
	MaxDevices   *int64     `json:"max_devices"` // nil = global default
	ReferralCode string     `json:"referral_code"`
	ReferredBy   *int64     `json:"referred_by"`
	CreatedAt    time.Time  `json:"created_at"`
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
	// ConfigFetchedAt records when a rendered config was last delivered to
	// the client (/config or /qr.png). Multi-node support uses it to tell
	// "this credential was never fetched after the endpoint moved" apart
	// from "the user re-downloaded after the switch". nil = never fetched.
	ConfigFetchedAt *time.Time `json:"config_fetched_at"`
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

// JoinCode is a short-lived one-time quick-connect code for one device. The
// owner mints it in the Mini App (or an admin in the panel) and hands it to
// the person holding the client; the client exchanges it for the rendered
// config via the public POST /api/join/{code}. Unlike an invite (which
// creates an account), a join code never authenticates anyone — it only
// carries an already-issued credential. See docs/join.md.
type JoinCode struct {
	Code      string     `json:"code"`
	DeviceID  int64      `json:"device_id"`
	CreatedBy int64      `json:"created_by"` // 0 = created with the static admin token
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
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
	expires_at  INTEGER,
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

CREATE TABLE IF NOT EXISTS join_codes (
	code       TEXT PRIMARY KEY,
	device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
	created_by INTEGER NOT NULL,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at    INTEGER
);

CREATE TABLE IF NOT EXISTS payments (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	amount     INTEGER NOT NULL CHECK (amount > 0), -- minor units (kopecks/cents)
	days       INTEGER NOT NULL DEFAULT 0,          -- subscription days granted (0 = no extension)
	note       TEXT    NOT NULL DEFAULT '',
	created_by INTEGER NOT NULL,                    -- 0 = created with the static admin token
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_payments_user ON payments(user_id);

CREATE TABLE IF NOT EXISTS referral_earnings (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	beneficiary_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	payment_id     INTEGER NOT NULL REFERENCES payments(id) ON DELETE CASCADE,
	payer_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	level          INTEGER NOT NULL CHECK (level IN (1,2)),
	amount         INTEGER NOT NULL, -- minor units
	created_at     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_referral_earnings_beneficiary ON referral_earnings(beneficiary_id);

CREATE TABLE IF NOT EXISTS referral_payouts (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	amount     INTEGER NOT NULL CHECK (amount > 0), -- minor units
	note       TEXT    NOT NULL DEFAULT '',
	created_by INTEGER NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_referral_payouts_user ON referral_payouts(user_id);

CREATE TABLE IF NOT EXISTS nodes (
	id                        INTEGER PRIMARY KEY AUTOINCREMENT,
	name                      TEXT    NOT NULL UNIQUE,
	host                      TEXT    NOT NULL,
	wg_port                   INTEGER NOT NULL DEFAULT 51820,
	xray_port                 INTEGER,
	token                     TEXT    NOT NULL UNIQUE,
	server_public_key         TEXT,
	xray_reality_public_key   TEXT,
	xray_short_id             TEXT,
	status                    TEXT    NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','ready','blocked')),
	last_seen_at              INTEGER,
	created_at                INTEGER NOT NULL
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
	if err := migrateUsersExpiry(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate users.expires_at: %w", err)
	}
	if err := migrateUsersMaxDevices(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate users.max_devices: %w", err)
	}
	if err := migrateDevicesConfigFetched(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate devices.config_fetched_at: %w", err)
	}
	st := &Store{db: db}
	if err := migrateUsersReferrals(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate users referrals: %w", err)
	}
	if err := st.backfillReferralCodes(); err != nil {
		db.Close()
		return nil, fmt.Errorf("backfill referral codes: %w", err)
	}
	return st, nil
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

// migrateUsersExpiry adds the users.expires_at column (manual monetization:
// the deadline until which the account's tunnel keeps working, see
// docs/billing.md) to databases created before the billing feature. Same
// pragma-gate pattern as migrateDevicesProtocol: a plain nullable INTEGER
// column, so ALTER TABLE is enough — no table rebuild needed. Existing rows
// read back as NULL (= unlimited), which preserves today's behaviour.
func migrateUsersExpiry(db *sql.DB) error {
	rows, err := db.Query(`pragma table_info(users)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasExpiry := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == "expires_at" {
			hasExpiry = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if hasExpiry {
		return nil // already migrated (normal case on every start)
	}
	_, err = db.Exec(`ALTER TABLE users ADD COLUMN expires_at INTEGER`)
	return err
}

// migrateUsersMaxDevices adds the users.max_devices column (per-user device
// caps with owner cascade). Same pragma-gate pattern as migrateUsersExpiry: a
// plain nullable INTEGER column, ALTER TABLE is enough. Existing rows read
// back as NULL (= global default from FIBR_MAX_DEVICES), which preserves
// today's behaviour.
func migrateUsersMaxDevices(db *sql.DB) error {
	rows, err := db.Query(`pragma table_info(users)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasMax := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == "max_devices" {
			hasMax = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if hasMax {
		return nil // already migrated (normal case on every start)
	}
	_, err = db.Exec(`ALTER TABLE users ADD COLUMN max_devices INTEGER`)
	return err
}

// migrateDevicesConfigFetched adds the devices.config_fetched_at column
// (timestamp of the last delivered rendered config; see Device). Nullable
// INTEGER, pragma-gated ALTER, existing rows read back as NULL.
func migrateDevicesConfigFetched(db *sql.DB) error {
	rows, err := db.Query(`pragma table_info(devices)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasCol := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == "config_fetched_at" {
			hasCol = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if hasCol {
		return nil // already migrated (normal case on every start)
	}
	_, err = db.Exec(`ALTER TABLE devices ADD COLUMN config_fetched_at INTEGER`)
	return err
}

// migrateUsersReferrals adds the referral-program columns (see docs/
// referrals.md): users.referral_code (the account's permanent invite code)
// and users.referred_by (whose code the account signed up with). Both are
// plain nullable columns, so pragma-gated ALTER TABLE is enough. The unique
// index is partial: pre-backfill rows have NULL codes and must not collide.
// Idempotent per column: the index has IF NOT EXISTS, so the second Open()
// skips both ALTERs and the index creation.
func migrateUsersReferrals(db *sql.DB) error {
	rows, err := db.Query(`pragma table_info(users)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasCode, hasBy := false, false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		switch name {
		case "referral_code":
			hasCode = true
		case "referred_by":
			hasBy = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasCode {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN referral_code TEXT`); err != nil {
			return err
		}
	}
	if !hasBy {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN referred_by INTEGER REFERENCES users(id) ON DELETE SET NULL`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_referral_code ON users(referral_code) WHERE referral_code IS NOT NULL`)
	return err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

/* ---------------------------- users ---------------------------- */

const userCols = `id, telegram_id, username, role, owner_id, disabled, expires_at, max_devices, referral_code, referred_by, created_at`

// CreateUser inserts a user; ID and CreatedAt are filled in. OwnerID (if
// set) records who created the account. ExpiresAt (if set) is the account's
// subscription deadline (nil = unlimited). MaxDevices (if set) is the
// account's device cap (nil = global default). ReferredBy (if set) is the
// referral attribution (whose code the account signed up with). A unique
// referral code is minted here; collisions retry with a fresh code (the
// alphabet is huge, so this is a safety net, not an expectation).
func (s *Store) CreateUser(u *User) error {
	if u.Role == "" {
		u.Role = RoleUser
	}
	now := time.Now().Unix()
	var tid, oid, exp, maxd, rby any
	if u.TelegramID != nil {
		tid = *u.TelegramID
	}
	if u.OwnerID != nil {
		oid = *u.OwnerID
	}
	if u.ExpiresAt != nil {
		exp = u.ExpiresAt.Unix()
	}
	if u.MaxDevices != nil {
		maxd = *u.MaxDevices
	}
	if u.ReferredBy != nil {
		rby = *u.ReferredBy
	}
	// A fresh referral code per attempt so a UNIQUE collision (code or
	// telegram_id) retries with new randomness; the telegram_id conflict
	// still surfaces after the attempts run out.
	var id int64
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		code, err := genReferralCode()
		if err != nil {
			return err
		}
		res, err := s.db.Exec(
			`INSERT INTO users (telegram_id, username, role, owner_id, disabled, expires_at, max_devices, referral_code, referred_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			tid, u.Username, u.Role, oid, b2i(u.Disabled), exp, maxd, code, rby, now,
		)
		if err != nil {
			lastErr = err
			continue
		}
		id, err = res.LastInsertId()
		if err != nil {
			return err
		}
		u.ReferralCode = code
		u.ID = id
		u.CreatedAt = time.Unix(now, 0)
		return nil
	}
	return fmt.Errorf("create user: %w", lastErr)
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

// SetUserExpiry sets the account's subscription deadline (unix seconds);
// a nil unix clears it (= unlimited). Callers are expected to resync peers
// afterwards so the change is applied to the tunnel (see docs/billing.md).
func (s *Store) SetUserExpiry(id int64, unix *int64) error {
	var exp any
	if unix != nil {
		exp = *unix
	}
	res, err := s.db.Exec(`UPDATE users SET expires_at = ? WHERE id = ?`, exp, id)
	return touched(res, err)
}

// SetUserMaxDevices sets the account's device cap; nil clears it (= global
// default from FIBR_MAX_DEVICES). No tunnel resync is needed — the cap only
// gates future device creation.
func (s *Store) SetUserMaxDevices(id int64, n *int64) error {
	var v any
	if n != nil {
		v = *n
	}
	res, err := s.db.Exec(`UPDATE users SET max_devices = ? WHERE id = ?`, v, id)
	return touched(res, err)
}

// EffectiveMaxDevices resolves the device cap that actually governs an
// account: the minimum of its own max_devices and its owner-admin's, over
// the non-NULL ones; when neither side is set the global fallback applies.
// This mirrors the EffectiveExpiry cascade (an unpaid admin's whole branch is
// capped with them), with one difference: a set value never gets widened by
// the fallback, so the fallback is only a last resort. 0 is a real value
// meaning "no new devices".
func EffectiveMaxDevices(own, owner *int64, fallback int) int {
	limit := fallback
	if own != nil {
		limit = int(*own)
	}
	if owner != nil && int(*owner) < limit {
		limit = int(*owner)
	}
	return limit
}

// EffectiveExpiry resolves the deadline that actually governs an account:
// its own expires_at OR its owner-admin's, whichever comes first. nil on a
// side means "no deadline" and is ignored; both nil = unlimited. This is the
// cascade that makes an unpaid admin's whole branch expire with them.
func EffectiveExpiry(own, owner *time.Time) *time.Time {
	switch {
	case own == nil:
		return owner
	case owner == nil:
		return own
	case own.Before(*owner):
		return own
	default:
		return owner
	}
}

// ExpiredAt reports whether a resolved deadline has passed at now. A nil
// deadline never expires. The account is expired from the deadline second
// onwards (deadline <= now).
func ExpiredAt(deadline *time.Time, now time.Time) bool {
	return deadline != nil && !deadline.After(now)
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
	var tid, oid, exp, maxd, rby sql.NullInt64
	var code sql.NullString
	var disabled int
	var created int64
	if err := r.Scan(&u.ID, &tid, &u.Username, &u.Role, &oid, &disabled, &exp, &maxd, &code, &rby, &created); err != nil {
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
	if exp.Valid && exp.Int64 > 0 {
		e := time.Unix(exp.Int64, 0)
		u.ExpiresAt = &e
	}
	if maxd.Valid {
		m := maxd.Int64
		u.MaxDevices = &m
	}
	u.ReferralCode = code.String
	if rby.Valid {
		rb := rby.Int64
		u.ReferredBy = &rb
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

const deviceCols = `id, user_id, name, protocol, public_key, private_key, preshared_key, ipv4, enabled, created_at, last_handshake, last_rx, last_tx, total_rx, total_tx, config_fetched_at`

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

// ListActiveDevices returns the devices whose tunnel is still paid for and
// therefore must stay on the WireGuard interface: device enabled, the owning
// user not past their subscription deadline and, for reseller users, the
// creating admin's branch not past its own either (EffectiveExpiry cascade;
// see docs/billing.md). Expiry filtering lives here so callers (wg sync)
// stay dumb. Xray devices are included — the xray manager applies its own
// protocol filter on top.
func (s *Store) ListActiveDevices(now time.Time) ([]Device, error) {
	rows, err := s.db.Query(`SELECT d.id, d.user_id, d.name, d.protocol, d.public_key, d.private_key, d.preshared_key, d.ipv4,
		d.enabled, d.created_at, d.last_handshake, d.last_rx, d.last_tx, d.total_rx, d.total_tx, d.config_fetched_at
		FROM devices d
		JOIN users u ON u.id = d.user_id
		LEFT JOIN users o ON o.id = u.owner_id
		WHERE d.enabled = 1
		  AND (u.expires_at IS NULL OR u.expires_at > ?)
		  AND (o.expires_at IS NULL OR o.expires_at > ?)
		ORDER BY d.id`,
		now.Unix(), now.Unix())
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

// ListExpiredUserIDs returns the ids of every account whose effective
// deadline (own expires_at OR owner's, whichever is earlier) has passed.
// This is exactly the set whose devices ListActiveDevices drops.
func (s *Store) ListExpiredUserIDs(now time.Time) ([]int64, error) {
	rows, err := s.db.Query(`SELECT u.id FROM users u
		LEFT JOIN users o ON o.id = u.owner_id
		WHERE (u.expires_at IS NOT NULL AND u.expires_at <= ?)
		   OR (o.expires_at IS NOT NULL AND o.expires_at <= ?)
		ORDER BY u.id`,
		now.Unix(), now.Unix())
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

// TouchDeviceConfigFetched stamps the "rendered config last delivered"
// timestamp (see Device.ConfigFetchedAt). Called after a successful /config
// or /qr.png render; failures are logged by the caller, never fatal.
func (s *Store) TouchDeviceConfigFetched(id int64, now time.Time) error {
	res, err := s.db.Exec(`UPDATE devices SET config_fetched_at = ? WHERE id = ?`, now.Unix(), id)
	return touched(res, err)
}

func scanDevice(r interface{ Scan(...any) error }) (*Device, error) {
	var d Device
	var enabled int
	var created, hs int64
	var fetched sql.NullInt64
	if err := r.Scan(&d.ID, &d.UserID, &d.Name, &d.Protocol, &d.PublicKey, &d.PrivateKey, &d.PresharedKey, &d.IPv4,
		&enabled, &created, &hs, &d.LastRx, &d.LastTx, &d.TotalRx, &d.TotalTx, &fetched); err != nil {
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
	if fetched.Valid && fetched.Int64 > 0 {
		t := time.Unix(fetched.Int64, 0)
		d.ConfigFetchedAt = &t
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

/* ---------------------------- join codes ---------------------------- */

// CreateJoinCode inserts a quick-connect code; Code, DeviceID, CreatedBy and
// ExpiresAt must be set by the caller, timestamps are filled in here.
func (s *Store) CreateJoinCode(jc *JoinCode) error {
	now := time.Now().Unix()
	if _, err := s.db.Exec(
		`INSERT INTO join_codes (code, device_id, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		jc.Code, jc.DeviceID, jc.CreatedBy, now, jc.ExpiresAt.Unix(),
	); err != nil {
		return err
	}
	jc.CreatedAt = time.Unix(now, 0)
	return nil
}

// GetJoinCode returns a join code by value, or ErrNotFound.
func (s *Store) GetJoinCode(code string) (*JoinCode, error) {
	row := s.db.QueryRow(`SELECT code, device_id, created_by, created_at, expires_at, used_at FROM join_codes WHERE code = ?`, code)
	return scanJoinCode(row)
}

// ConsumeJoinCode atomically burns a join code and returns it. The UPDATE
// only matches unused codes whose TTL window is still open, so exactly one
// concurrent exchange can win. ErrJoinCodeUsed covers both "already
// exchanged" and "past its TTL" — the caller cannot distinguish (and should
// not: both mean "get a fresh code from the owner").
func (s *Store) ConsumeJoinCode(code string) (*JoinCode, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`UPDATE join_codes SET used_at = ? WHERE code = ? AND used_at IS NULL AND expires_at > ?`,
		now, code, now,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetJoinCode(code); err != nil {
			return nil, err // ErrNotFound (or a storage error)
		}
		return nil, ErrJoinCodeUsed
	}
	return s.GetJoinCode(code)
}

// DeleteStaleJoinCodes drops consumed and long-expired codes. Best-effort
// housekeeping: called when a new code is minted so the table stays tiny.
func (s *Store) DeleteStaleJoinCodes() error {
	_, err := s.db.Exec(`DELETE FROM join_codes WHERE used_at IS NOT NULL OR expires_at <= ?`, time.Now().Unix())
	return err
}

func scanJoinCode(r interface{ Scan(...any) error }) (*JoinCode, error) {
	var jc JoinCode
	var created, expires int64
	var usedAt sql.NullInt64
	if err := r.Scan(&jc.Code, &jc.DeviceID, &jc.CreatedBy, &created, &expires, &usedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	jc.CreatedAt = time.Unix(created, 0)
	jc.ExpiresAt = time.Unix(expires, 0)
	if usedAt.Valid && usedAt.Int64 > 0 {
		t := time.Unix(usedAt.Int64, 0)
		jc.UsedAt = &t
	}
	return &jc, nil
}

/* ---------------------------- settings ---------------------------- */

// Well-known settings keys. active_node_id holds the id of the node whose
// endpoint goes into rendered client configs (0/absent = the local built-in
// node; see EnsureLocalNode). peer_revision is a monotonic counter bumped on
// every WireGuard peer mutation so remote node agents know to re-pull.
// endpoint_changed_at (unix seconds) is bumped when the active endpoint
// moves — clients compare it to decide whether a re-download is needed.
//
// The referral_* / currency / contact_info keys drive the referral program
// (see docs/referrals.md). All are superadmin-editable over the API; absent
// keys fall back to the defaults noted below.
const (
	SettingActiveNodeID      = "active_node_id"
	SettingPeerRevision      = "peer_revision"
	SettingEndpointChangedAt = "endpoint_changed_at"

	SettingReferralEnabled   = "referral_enabled"    // "1"/"0"; default on
	SettingReferralPercentL1 = "referral_percent_l1" // default 15
	SettingReferralPercentL2 = "referral_percent_l2" // default 5
	SettingCurrency          = "currency"            // display suffix, e.g. "₽" or "USDT"; default ""
	SettingContactInfo       = "contact_info"        // how to reach the installation owner (payouts, renewals)
)

// Referral defaults used when the matching settings key is absent.
const (
	ReferralPercentL1Default = 15
	ReferralPercentL2Default = 5
)

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

// GetSettingInt returns a settings value parsed as an integer. A missing key
// reads as 0 without an error (callers treat "absent" as the zero state:
// revision 0, local node); only a malformed or failing read errors.
func (s *Store) GetSettingInt(key string) (int64, error) {
	v, err := s.GetSetting(key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	n, perr := strconv.ParseInt(v, 10, 64)
	if perr != nil {
		return 0, fmt.Errorf("setting %s: %q is not an integer", key, v)
	}
	return n, nil
}

// SetSettingInt upserts a settings value in integer form.
func (s *Store) SetSettingInt(key string, v int64) error {
	return s.SetSetting(key, strconv.FormatInt(v, 10))
}

// BumpPeerRevision increments the peer revision counter and returns the new
// value. Store connections are serialized (MaxOpenConns=1), so the
// read-modify-write cannot interleave.
func (s *Store) BumpPeerRevision() (int64, error) {
	cur, err := s.GetSettingInt(SettingPeerRevision)
	if err != nil {
		return 0, err
	}
	next := cur + 1
	if err := s.SetSettingInt(SettingPeerRevision, next); err != nil {
		return 0, err
	}
	return next, nil
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
