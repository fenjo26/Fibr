package dns

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// BuiltIn list names.
const (
	BuiltInAdblock = "adblock"
	BuiltInAdult   = "adult"
)

// Default BuiltIn list URLs (both MIT-licensed StevenBlack hosts files); the
// Config fields (FIBR_DNS_LISTS_ADBLOCK / _ADULT) can override them.
const (
	defaultAdblockURL = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"
	defaultAdultURL   = "https://raw.githubusercontent.com/StevenBlack/hosts/master/alternates/porn/hosts"
)

// refreshInterval is the automatic list update period (docs/dns-games.md §1).
const refreshInterval = 24 * time.Hour

// maxListSize caps a single downloaded list (the biggest public hosts lists
// are well under 100 MB).
const maxListSize = 128 << 20

// nameRe constrains list names: they become file names in the cache dir.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ListEntry is one blocklist row as the admin UI sees it.
type ListEntry struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Enabled   bool      `json:"enabled"`
	BuiltIn   bool      `json:"builtin"`
	LastOK    time.Time `json:"last_ok"`
	LastCheck time.Time `json:"last_check"`
}

// ListManager owns the dynamic blocklist set ("as in AdGuard Home"): two
// BuiltIn lists plus any number of user-supplied URLs, persisted in
// <dataDir>/lists.db, downloaded with ETag revalidation into
// <dataDir>/lists/<name>.list and parsed into the Filter's named sets.
type ListManager struct {
	mu     sync.Mutex
	db     *sql.DB
	dir    string // cache dir: <dataDir>/lists
	filter *Filter
	log    *slog.Logger
	client *http.Client
}

// NewListManager opens (creating if needed) the list database, ensures the
// BuiltIn rows exist with the current URLs, and loads whatever cache is on
// disk into the filter — offline start is not an error, the resolver simply
// runs on stale rules (or none).
func NewListManager(dataDir string, filter *Filter, adblockURL, adultURL string, log *slog.Logger) (*ListManager, error) {
	if adblockURL == "" {
		adblockURL = defaultAdblockURL
	}
	if adultURL == "" {
		adultURL = defaultAdultURL
	}
	if adblockURL == adultURL {
		return nil, fmt.Errorf("adblock and adult list URLs must differ (both %q)", adblockURL)
	}
	if log == nil {
		log = slog.Default()
	}
	dir := filepath.Join(dataDir, "lists")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create list cache dir: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "lists.db")+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open lists.db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite writes are serialized; keeps things simple and safe
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS dns_lists(
		name       TEXT PRIMARY KEY,
		url        TEXT NOT NULL UNIQUE,
		enabled    INTEGER NOT NULL DEFAULT 1,
		builtin    INTEGER NOT NULL DEFAULT 0,
		etag       TEXT NOT NULL DEFAULT '',
		last_ok    INTEGER NOT NULL DEFAULT 0,
		last_check INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate dns_lists: %w", err)
	}

	m := &ListManager{
		db:     db,
		dir:    dir,
		filter: filter,
		log:    log,
		client: &http.Client{Timeout: 30 * time.Second},
	}

	// BuiltIn rows: keep the user's enabled flag across restarts, but always
	// track the currently configured URL (env override).
	for name, u := range map[string]string{BuiltInAdblock: adblockURL, BuiltInAdult: adultURL} {
		if _, err := db.Exec(`INSERT INTO dns_lists(name, url, enabled, builtin)
			VALUES(?, ?, 1, 1)
			ON CONFLICT(name) DO UPDATE SET url=excluded.url`, name, u); err != nil {
			db.Close()
			return nil, fmt.Errorf("ensure builtin list %s: %w", name, err)
		}
	}

	m.rebuildSets()
	return m, nil
}

// Close closes the database.
func (m *ListManager) Close() error { return m.db.Close() }

// Run performs an initial best-effort refresh and then re-fetches every
// enabled list every 24h until ctx is cancelled. A failed refresh leaves the
// previous rules in place (offline start is never fatal).
func (m *ListManager) Run(ctx context.Context) {
	if err := m.RefreshAll(ctx); err != nil {
		m.log.Warn("dns lists: initial refresh failed, running on cached rules", "error", err)
	}
	t := time.NewTicker(refreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.RefreshAll(ctx); err != nil {
				m.log.Warn("dns lists: scheduled refresh failed, keeping previous rules", "error", err)
			}
		}
	}
}

// Lists returns all list entries, BuiltIn first.
func (m *ListManager) Lists() []ListEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := m.db.Query(`SELECT name, url, enabled, builtin, last_ok, last_check
		FROM dns_lists ORDER BY builtin DESC, name`)
	if err != nil {
		m.log.Warn("dns lists: query failed", "error", err)
		return nil
	}
	defer rows.Close()
	var out []ListEntry
	for rows.Next() {
		var e ListEntry
		var enabled, builtin int
		var lastOK, lastCheck int64
		if err := rows.Scan(&e.Name, &e.URL, &enabled, &builtin, &lastOK, &lastCheck); err != nil {
			m.log.Warn("dns lists: scan failed", "error", err)
			return out
		}
		e.Enabled, e.BuiltIn = enabled == 1, builtin == 1
		e.LastOK, e.LastCheck = time.Unix(lastOK, 0), time.Unix(lastCheck, 0)
		out = append(out, e)
	}
	return out
}

// ErrBuiltIn is returned on attempts to remove a BuiltIn list.
var ErrBuiltIn = errors.New("built-in list cannot be removed, only disabled")

// AddList registers a custom list and tries to download it right away
// (best-effort: even if the download fails the entry stays for the ticker to
// retry).
func (m *ListManager) AddList(name, rawURL string) error {
	name = strings.TrimSpace(name)
	if !nameRe.MatchString(name) {
		return fmt.Errorf("list name must be 1-64 chars of letters, digits, dots, dashes or underscores")
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("list URL must be a valid http(s) URL")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var dup int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM dns_lists WHERE url = ? OR name = ?`, u.String(), name).Scan(&dup); err != nil {
		return fmt.Errorf("check duplicate list: %w", err)
	}
	if dup > 0 {
		return fmt.Errorf("a list with this URL or name already exists")
	}
	if _, err := m.db.Exec(`INSERT INTO dns_lists(name, url, enabled, builtin) VALUES(?, ?, 1, 0)`, name, u.String()); err != nil {
		return fmt.Errorf("insert list: %w", err)
	}
	if err := m.refreshByName(context.Background(), name); err != nil {
		m.log.Warn("dns lists: initial download failed, will retry on next refresh", "name", name, "error", err)
	}
	m.rebuildSets()
	return nil
}

// RemoveList deletes a custom list and its cache.
func (m *ListManager) RemoveList(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var builtin int
	if err := m.db.QueryRow(`SELECT builtin FROM dns_lists WHERE name = ?`, name).Scan(&builtin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("list %q not found", name)
		}
		return fmt.Errorf("lookup list: %w", err)
	}
	if builtin == 1 {
		return ErrBuiltIn
	}
	if _, err := m.db.Exec(`DELETE FROM dns_lists WHERE name = ?`, name); err != nil {
		return fmt.Errorf("delete list: %w", err)
	}
	_ = os.Remove(m.listPath(name))
	_ = os.Remove(m.etagPath(name))
	m.rebuildSets()
	return nil
}

// SetListEnabled toggles a list without deleting its cache.
func (m *ListManager) SetListEnabled(name string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	res, err := m.db.Exec(`UPDATE dns_lists SET enabled = ? WHERE name = ?`, boolToInt(enabled), name)
	if err != nil {
		return fmt.Errorf("update list: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("list %q not found", name)
	}
	m.rebuildSets()
	return nil
}

// RefreshAll re-validates every enabled list (the UI "Refresh now" button).
// Errors of individual lists are joined; a failing list keeps its old rules.
func (m *ListManager) RefreshAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for _, e := range m.entriesLocked() {
		if !e.Enabled {
			continue
		}
		if err := m.refreshByName(ctx, e.Name); err != nil {
			m.log.Warn("dns lists: refresh failed", "name", e.Name, "error", err)
			errs = append(errs, fmt.Errorf("%s: %w", e.Name, err))
		}
	}
	m.rebuildSets()
	return errors.Join(errs...)
}

// RefreshOne force-refreshes a single list outside the 24h ticker.
func (m *ListManager) RefreshOne(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.refreshByName(ctx, name); err != nil {
		return err
	}
	m.rebuildSets()
	return nil
}

// refreshByName downloads one list honoring ETag revalidation and records the
// outcome in the database. Caller holds m.mu.
func (m *ListManager) refreshByName(ctx context.Context, name string) error {
	var listURL, etag string
	err := m.db.QueryRow(`SELECT url, etag FROM dns_lists WHERE name = ?`, name).Scan(&listURL, &etag)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("list %q not found", name)
	}
	if err != nil {
		return fmt.Errorf("lookup list: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "Fibr-DNS/1.0")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	now := time.Now()
	switch resp.StatusCode {
	case http.StatusNotModified:
		_, uerr := m.db.Exec(`UPDATE dns_lists SET last_ok = ?, last_check = ? WHERE name = ?`,
			now.Unix(), now.Unix(), name)
		return uerr
	case http.StatusOK:
	default:
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxListSize))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := atomicWrite(m.listPath(name), body); err != nil {
		return fmt.Errorf("store list: %w", err)
	}
	newETag := resp.Header.Get("ETag")
	if err := atomicWrite(m.etagPath(name), []byte(newETag)); err != nil {
		return fmt.Errorf("store etag: %w", err)
	}
	_, uerr := m.db.Exec(`UPDATE dns_lists SET etag = ?, last_ok = ?, last_check = ? WHERE name = ?`,
		newETag, now.Unix(), now.Unix(), name)
	return uerr
}

// rebuildSets re-parses the cached files of every enabled list into the
// filter's named sets. Caller holds m.mu.
func (m *ListManager) rebuildSets() {
	builtin := map[string]string{BuiltInAdblock: SetAdblock, BuiltInAdult: SetAdult}
	var customs []string
	for _, e := range m.entriesLocked() {
		if !e.Enabled {
			continue
		}
		if set, ok := builtin[e.Name]; ok {
			m.loadFile(set, e.Name)
			continue
		}
		customs = append(customs, e.Name)
	}
	if len(customs) > 0 {
		readers := make([]io.Reader, 0, len(customs))
		for _, name := range customs {
			if f, err := os.Open(m.listPath(name)); err == nil {
				readers = append(readers, f)
			}
		}
		if len(readers) > 0 {
			if _, err := m.filter.LoadSet(SetCustom, readers...); err != nil {
				m.log.Warn("dns lists: custom set reload failed", "error", err)
			}
			closeAll(readers)
		}
	} else {
		m.filter.DropSet(SetCustom)
	}
	for name, set := range builtin {
		m.dropIfAbsent(name, set)
	}
}

// dropIfAbsent disables a BuiltIn filter set whose list is missing or
// disabled (no cached file -> nothing to serve).
func (m *ListManager) dropIfAbsent(listName, setName string) {
	for _, e := range m.entriesLocked() {
		if e.Name == listName {
			if !e.Enabled {
				m.filter.DropSet(setName)
			}
			return
		}
	}
}

func (m *ListManager) loadFile(set, name string) {
	f, err := os.Open(m.listPath(name))
	if err != nil {
		m.filter.DropSet(set)
		return
	}
	defer f.Close()
	if _, err := m.filter.LoadSet(set, f); err != nil {
		m.log.Warn("dns lists: set reload failed", "set", set, "error", err)
	}
}

// entriesLocked reads all rows; caller holds m.mu.
func (m *ListManager) entriesLocked() []ListEntry {
	rows, err := m.db.Query(`SELECT name, url, enabled, builtin, last_ok, last_check
		FROM dns_lists ORDER BY builtin DESC, name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []ListEntry
	for rows.Next() {
		var e ListEntry
		var enabled, builtin int
		var lastOK, lastCheck int64
		if err := rows.Scan(&e.Name, &e.URL, &enabled, &builtin, &lastOK, &lastCheck); err != nil {
			return out
		}
		e.Enabled, e.BuiltIn = enabled == 1, builtin == 1
		e.LastOK, e.LastCheck = time.Unix(lastOK, 0), time.Unix(lastCheck, 0)
		out = append(out, e)
	}
	return out
}

func (m *ListManager) listPath(name string) string { return filepath.Join(m.dir, name+".list") }
func (m *ListManager) etagPath(name string) string { return filepath.Join(m.dir, name+".etag") }

// atomicWrite writes data via a temp file + rename so a crash never leaves a
// half-written list behind.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func closeAll(readers []io.Reader) {
	for _, r := range readers {
		if c, ok := r.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
