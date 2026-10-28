package store

// Web logins: an optional login + password pair per account, set by the
// person in the web cabinet profile after they got in with their personal
// code / login link (docs: invite link -> code -> own login and password).
// Only a salted PBKDF2-SHA256 hash is stored (api package hashes; the store
// keeps opaque strings). Logins are unique, case-insensitive (stored
// lower-cased by the caller).

import (
	"database/sql"
	"errors"
	"strings"
)

// ErrLoginTaken is returned by SetUserLogin when another account owns it.
var ErrLoginTaken = errors.New("login taken")

// migrateUsersLogin adds users.login + users.password_hash (pragma-gated
// like the other column migrations) and a partial unique index on login.
func migrateUsersLogin(db *sql.DB) error {
	rows, err := db.Query(`pragma table_info(users)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	has := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return err
		}
		has[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if !has["login"] {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN login TEXT`); err != nil {
			return err
		}
	}
	if !has["password_hash"] {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN password_hash TEXT`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_login ON users(login) WHERE login IS NOT NULL`)
	return err
}

// SetUserLogin sets (or replaces) the account's login and password hash.
func (s *Store) SetUserLogin(id int64, login, passwordHash string) error {
	res, err := s.db.Exec(`UPDATE users SET login = ?, password_hash = ? WHERE id = ?`, login, passwordHash, id)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return ErrLoginTaken
	}
	return touched(res, err)
}

// GetUserByLogin returns the account and its password hash for a login.
func (s *Store) GetUserByLogin(login string) (*User, string, error) {
	var hash sql.NullString
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE login = ?`, login).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE login = ?`, login))
	if err != nil {
		return nil, "", err
	}
	return u, hash.String, nil
}

// UserLogin returns the account's login ("" when none is set).
func (s *Store) UserLogin(id int64) (string, error) {
	var v sql.NullString
	err := s.db.QueryRow(`SELECT login FROM users WHERE id = ?`, id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v.String, err
}
