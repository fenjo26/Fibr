package dns

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

const (
	// ringSize is the in-memory buffer between a query and the batch insert.
	ringSize = 10_000
	// flushInterval batches ring contents into sqlite.
	flushInterval = 5 * time.Second
	// pruneInterval trims rows older than the retention.
	pruneInterval = time.Hour
	// statsWindow is the aggregation period of Stats24h.
	statsWindow = 24 * time.Hour
)

// Action values recorded in the query log.
const (
	ActionAllow      = "allow"
	ActionBlock      = "block"
	ActionSafesearch = "safesearch"
	ActionSchedule   = "schedule"
)

// LogEntry is one resolved (or refused) query.
type LogEntry struct {
	TS       time.Time `json:"ts"`
	DeviceIP string    `json:"device_ip"`
	QName    string    `json:"qname"`
	QType    uint16    `json:"qtype"`
	Action   string    `json:"action"`
	Profile  string    `json:"profile"`
	List     string    `json:"list"`
}

// DomainCount is a top-domain row of Stats24h.
type DomainCount struct {
	Domain string `json:"domain"`
	Count  int64  `json:"count"`
}

// Stats is the 24h aggregate for the admin dashboard.
type Stats struct {
	Total      int64            `json:"total"`
	ByAction   map[string]int64 `json:"by_action"`
	TopBlocked []DomainCount    `json:"top_blocked"`
}

// QueryLog buffers queries in a ring and batch-persists them into
// <dataDir>/queries.db, pruning old rows hourly.
type QueryLog struct {
	mu   sync.Mutex
	ring [ringSize]LogEntry
	head int // next write position
	size int // valid entries, head-size .. head-1 (mod ring)

	closed bool // guards flush/prune against the Run goroutine's final pass

	db        *sql.DB
	retention time.Duration
	log       *slog.Logger
}

// OpenQueryLog opens (creating if needed) the query log database.
func OpenQueryLog(dataDir string, retention time.Duration, log *slog.Logger) (*QueryLog, error) {
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create dns data dir: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "queries.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open queries.db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite writes are serialized; keeps things simple and safe
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS dns_query_log(
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		ts        INTEGER NOT NULL,
		device_ip TEXT NOT NULL,
		qname     TEXT NOT NULL,
		qtype     INTEGER NOT NULL,
		action    TEXT NOT NULL,
		profile   TEXT NOT NULL,
		list      TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate dns_query_log: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_dns_query_log_ts ON dns_query_log(ts)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("index dns_query_log(ts): %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_dns_query_log_ip_ts ON dns_query_log(device_ip, ts)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("index dns_query_log(device_ip,ts): %w", err)
	}
	return &QueryLog{db: db, retention: retention, log: log}, nil
}

// Log appends an entry to the ring; the oldest entry is overwritten when the
// ring is full (extreme bursts lose their head, never block the resolver).
func (q *QueryLog) Log(e LogEntry) {
	q.mu.Lock()
	q.ring[q.head] = e
	q.head = (q.head + 1) % ringSize
	if q.size < ringSize {
		q.size++
	}
	q.mu.Unlock()
}

// drain copies the ring out in chronological order and empties it.
func (q *QueryLog) drain() []LogEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]LogEntry, 0, q.size)
	start := (q.head - q.size + ringSize) % ringSize
	for i := 0; i < q.size; i++ {
		out = append(out, q.ring[(start+i)%ringSize])
	}
	q.size = 0
	return out
}

// Run flushes the ring every 5s and prunes old rows hourly; on ctx cancel it
// performs a final flush.
func (q *QueryLog) Run(ctx context.Context) {
	flushT := time.NewTicker(flushInterval)
	pruneT := time.NewTicker(pruneInterval)
	defer flushT.Stop()
	defer pruneT.Stop()
	for {
		select {
		case <-ctx.Done():
			q.flush()
			q.prune(context.Background())
			return
		case <-flushT.C:
			q.flush()
		case <-pruneT.C:
			q.prune(context.Background())
		}
	}
}

// flush batch-inserts the ring contents in one transaction.
func (q *QueryLog) flush() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.mu.Unlock()
	entries := q.drain()
	if len(entries) == 0 {
		return
	}
	tx, err := q.db.Begin()
	if err != nil {
		q.log.Warn("dns querylog: begin flush tx", "error", err)
		return
	}
	stmt, err := tx.Prepare(`INSERT INTO dns_query_log(ts, device_ip, qname, qtype, action, profile, list)
		VALUES(?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		q.log.Warn("dns querylog: prepare insert", "error", err)
		return
	}
	defer stmt.Close()
	for _, e := range entries {
		if _, err := stmt.Exec(e.TS.Unix(), e.DeviceIP, e.QName, e.QType, e.Action, e.Profile, e.List); err != nil {
			tx.Rollback()
			q.log.Warn("dns querylog: insert", "error", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		q.log.Warn("dns querylog: commit flush", "error", err)
	}
}

// prune deletes rows past the retention.
func (q *QueryLog) prune(ctx context.Context) {
	q.mu.Lock()
	closed := q.closed
	q.mu.Unlock()
	if closed {
		return
	}
	if _, err := q.db.ExecContext(ctx, `DELETE FROM dns_query_log WHERE ts < ?`,
		time.Now().Add(-q.retention).Unix()); err != nil {
		q.log.Warn("dns querylog: prune", "error", err)
	}
}

// Stats24h aggregates the last 24 hours: per-action counters and the top-50
// blocked domains. Reads the database, so entries still in the ring (up to
// 5s) are not included.
func (q *QueryLog) Stats24h() (Stats, error) {
	var st Stats = Stats{ByAction: map[string]int64{}}
	since := time.Now().Add(-statsWindow).Unix()

	rows, err := q.db.Query(`SELECT action, COUNT(*) FROM dns_query_log WHERE ts >= ? GROUP BY action`, since)
	if err != nil {
		return st, fmt.Errorf("querylog stats: %w", err)
	}
	for rows.Next() {
		var a string
		var n int64
		if err := rows.Scan(&a, &n); err != nil {
			rows.Close()
			return st, fmt.Errorf("querylog stats scan: %w", err)
		}
		st.ByAction[a] = n
		st.Total += n
	}
	rows.Close()

	rows, err = q.db.Query(`SELECT qname, COUNT(*) AS c FROM dns_query_log
		WHERE ts >= ? AND action = ? GROUP BY qname ORDER BY c DESC, qname LIMIT 50`,
		since, ActionBlock)
	if err != nil {
		return st, fmt.Errorf("querylog top: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d DomainCount
		if err := rows.Scan(&d.Domain, &d.Count); err != nil {
			return st, fmt.Errorf("querylog top scan: %w", err)
		}
		st.TopBlocked = append(st.TopBlocked, d)
	}
	return st, nil
}

// Entries returns the most recent rows, optionally filtered by device IP.
func (q *QueryLog) Entries(limit int, deviceIP string) ([]LogEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if deviceIP != "" {
		rows, err = q.db.Query(`SELECT ts, device_ip, qname, qtype, action, profile, list
			FROM dns_query_log WHERE device_ip = ? ORDER BY ts DESC LIMIT ?`, deviceIP, limit)
	} else {
		rows, err = q.db.Query(`SELECT ts, device_ip, qname, qtype, action, profile, list
			FROM dns_query_log ORDER BY ts DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("querylog entries: %w", err)
	}
	defer rows.Close()
	var out []LogEntry
	for rows.Next() {
		var e LogEntry
		var ts int64
		if err := rows.Scan(&ts, &e.DeviceIP, &e.QName, &e.QType, &e.Action, &e.Profile, &e.List); err != nil {
			return out, fmt.Errorf("querylog entries scan: %w", err)
		}
		e.TS = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, nil
}

// Close flushes pending entries and closes the database. The Run goroutine's
// final flush/prune become no-ops afterwards.
func (q *QueryLog) Close() error {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.flush()
	return q.db.Close()
}
