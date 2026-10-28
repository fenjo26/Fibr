package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrateDevicesProtocolAWG: a database whose devices.protocol CHECK
// only allows wg/xray is rebuilt to accept "awg" without losing rows or
// the join codes that reference devices.
func TestMigrateDevicesProtocolAWG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	u := &User{Username: "old"}
	if err := s.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	d := &Device{UserID: u.ID, Name: "phone", PublicKey: "pub1", PrivateKey: "priv1", IPv4: "10.8.0.2", Enabled: true}
	if err := s.CreateDevice(d); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Narrow the CHECK back to the pre-AWG text, as on existing servers.
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	var schemaSQL string
	if err := raw.QueryRow(`SELECT sql FROM sqlite_master WHERE name='devices'`).Scan(&schemaSQL); err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`PRAGMA foreign_keys=OFF`,
		`ALTER TABLE devices RENAME TO devices_tmp`,
		`CREATE TABLE devices (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			protocol TEXT NOT NULL DEFAULT 'wg' CHECK (protocol IN ('wg','xray')),
			public_key TEXT NOT NULL UNIQUE, private_key TEXT NOT NULL,
			preshared_key TEXT NOT NULL DEFAULT '', ipv4 TEXT NOT NULL UNIQUE,
			enabled INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL,
			last_handshake INTEGER NOT NULL DEFAULT 0, last_rx INTEGER NOT NULL DEFAULT 0,
			last_tx INTEGER NOT NULL DEFAULT 0, total_rx INTEGER NOT NULL DEFAULT 0,
			total_tx INTEGER NOT NULL DEFAULT 0, config_fetched_at INTEGER)`,
		`INSERT INTO devices SELECT * FROM devices_tmp`,
		`DROP TABLE devices_tmp`,
		`INSERT INTO join_codes (code, device_id, created_by, created_at, expires_at) VALUES ('JC1', 1, 0, 1, 9999999999)`,
	}
	for _, q := range stmts {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := raw.Exec(`INSERT INTO devices (user_id, name, protocol, public_key, private_key, ipv4, created_at) VALUES (1,'x','awg','p2','k2','10.9.0.2',1)`); err == nil {
		t.Fatal("setup: the narrowed CHECK should reject awg")
	}
	raw.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatalf("migrating open: %v", err)
	}
	defer s.Close()
	a := &Device{UserID: u.ID, Name: "amnezia", Protocol: ProtocolAWG, PublicKey: "pub2", PrivateKey: "priv2", IPv4: "10.9.0.2", Enabled: true}
	if err := s.CreateDevice(a); err != nil {
		t.Fatalf("awg device after migration: %v", err)
	}
	if got, err := s.GetDeviceByKey("pub1"); err != nil || got.Name != "phone" {
		t.Fatalf("old device lost: %v %+v", err, got)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM join_codes WHERE device_id = 1`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("join code lost: %v n=%d", err, n)
	}
	// Second open is a no-op.
	s.Close()
	if s, err = Open(path); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}
