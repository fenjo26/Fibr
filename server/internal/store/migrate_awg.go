package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// migrateDevicesProtocolAWG widens the devices.protocol CHECK constraint to
// accept "awg" (AmneziaWG as a per-device protocol). SQLite cannot alter a
// CHECK in place, so the table is rebuilt the documented way (sqlite.org
// "Making Other Kinds Of Table Schema Changes"): foreign keys off on one
// connection, create devices_new from the current CREATE text with the
// wider CHECK, copy, drop, rename, recreate the index — all in one
// transaction. A no-op once 'awg' is in the schema.
func migrateDevicesProtocolAWG(db *sql.DB) error {
	var current string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'devices'`).Scan(&current); err != nil {
		return err
	}
	if strings.Contains(current, "'awg'") {
		return nil
	}
	checkRe := regexp.MustCompile(`(?i)CHECK\s*\(\s*protocol\s+IN\s*\(\s*'wg'\s*,\s*'xray'\s*\)\s*\)`)
	if !checkRe.MatchString(current) {
		return nil // no protocol CHECK at all: nothing restricts "awg"
	}
	widened := checkRe.ReplaceAllString(current, "CHECK (protocol IN ('wg','xray','awg'))")
	nameRe := regexp.MustCompile(`(?is)^\s*CREATE\s+TABLE\s+(IF\s+NOT\s+EXISTS\s+)?["'\x60]?devices["'\x60]?`)
	if !nameRe.MatchString(widened) {
		return fmt.Errorf("unexpected devices CREATE statement: %s", current)
	}
	createNew := nameRe.ReplaceAllString(widened, "CREATE TABLE devices_new")

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`) //nolint:errcheck

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	steps := []string{
		createNew,
		`INSERT INTO devices_new SELECT * FROM devices`,
		`DROP TABLE devices`,
		`ALTER TABLE devices_new RENAME TO devices`,
		`CREATE INDEX IF NOT EXISTS idx_devices_user ON devices(user_id)`,
	}
	for _, q := range steps {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("%s: %w", strings.Fields(q)[0], err)
		}
	}
	return tx.Commit()
}
