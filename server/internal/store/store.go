// Package store persists users, devices and server settings in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// User is a Fibr account. Telegram-linked users are created lazily on first
// Mini App open or /start; admins can also create manual (panel-only) users.
type User struct {
	ID         int64     `json:"id"`
	TelegramID *int64    `json:"telegram_id"`
	Username   string    `json:"username"`
	Role       string    `json:"role"` // "user" | "admin"
	Disabled   bool      `json:"disabled"`
	CreatedAt  time.Time `json:"created_at"`
}

// Device is one WireGuard peer owned by a user. The private key is generated
// server-side so the panel can render full client configs and QR codes (the
// same trade-off wg-easy makes); see README for the security implications.
type Device struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	Name          string    `json:"name"`
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

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

const schema = `
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
CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id);

CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// Open opens (or creates) the database at path and applies migrations.
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
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

/* ---------------------------- users ---------------------------- */

// CreateUser inserts a user; ID and CreatedAt are filled in.
func (s *Store) CreateUser(u *User) error {
	if u.Role == "" {
		u.Role = "user"
	}
	now := time.Now().Unix()
	var tid any
	if u.TelegramID != nil {
		tid = *u.TelegramID
	}
	res, err := s.db.Exec(
		`INSERT INTO users (telegram_id, username, role, disabled, created_at) VALUES (?, ?, ?, ?, ?)`,
		tid, u.Username, u.Role, b2i(u.Disabled), now,
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
	row := s.db.QueryRow(`SELECT id, telegram_id, username, role, disabled, created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetUserByTelegramID returns a user by telegram id, or ErrNotFound.
func (s *Store) GetUserByTelegramID(tid int64) (*User, error) {
	row := s.db.QueryRow(`SELECT id, telegram_id, username, role, disabled, created_at FROM users WHERE telegram_id = ?`, tid)
	return scanUser(row)
}

// ListUsers returns all users ordered by id.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, telegram_id, username, role, disabled, created_at FROM users ORDER BY id`)
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

// SetUserRole sets the role ("user" or "admin").
func (s *Store) SetUserRole(id int64, role string) error {
	res, err := s.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
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
// is responsible for syncing WireGuard peers afterwards.
func (s *Store) DeleteUser(id int64) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return touched(res, err)
}

func scanUser(r interface{ Scan(...any) error }) (*User, error) {
	var u User
	var tid sql.NullInt64
	var disabled int
	var created int64
	if err := r.Scan(&u.ID, &tid, &u.Username, &u.Role, &disabled, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if tid.Valid {
		t := tid.Int64
		u.TelegramID = &t
	}
	u.Disabled = disabled != 0
	u.CreatedAt = time.Unix(created, 0)
	return &u, nil
}

/* ---------------------------- devices ---------------------------- */

// CreateDevice inserts a device; CreatedAt is filled in.
func (s *Store) CreateDevice(d *Device) error {
	now := time.Now().Unix()
	res, err := s.db.Exec(
		`INSERT INTO devices (user_id, name, public_key, private_key, preshared_key, ipv4, enabled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		d.UserID, d.Name, d.PublicKey, d.PrivateKey, d.PresharedKey, d.IPv4, b2i(d.Enabled), now,
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

const deviceCols = `id, user_id, name, public_key, private_key, preshared_key, ipv4, enabled, created_at, last_handshake, last_rx, last_tx, total_rx, total_tx`

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
	if err := r.Scan(&d.ID, &d.UserID, &d.Name, &d.PublicKey, &d.PrivateKey, &d.PresharedKey, &d.IPv4,
		&enabled, &created, &hs, &d.LastRx, &d.LastTx, &d.TotalRx, &d.TotalTx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.Enabled = enabled != 0
	d.CreatedAt = time.Unix(created, 0)
	if hs > 0 {
		d.LastHandshake = time.Unix(hs, 0)
	}
	return &d, nil
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
