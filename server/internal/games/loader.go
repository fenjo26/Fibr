package games

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Source list formats understood by the loader.
const (
	FormatPlain    = "plain"    // one CIDR per line, junk lines skipped
	FormatSSTap    = "sstap"    // SSTap-Rule file: # comments, [sections], CIDR lines
	FormatRIPEstat = "ripestat" // RIPEstat announced-prefixes JSON
)

const (
	// refreshInterval is the default period between catalog refreshes in Run.
	refreshInterval = 24 * time.Hour

	// maxSourceSize caps one source download (the real lists are a few KB).
	maxSourceSize = 8 << 20

	// cacheFile is the cache file name inside Options.DataDir.
	cacheFile = "games.json"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Source describes where the CIDR ranges of one game come from. Name is the
// game id (e.g. "cs2"); URLs are tried in order until one yields ranges.
type Source struct {
	Name       string   `json:"name"`
	GameName   string   `json:"game_name"`
	GameNameRu string   `json:"game_name_ru"`
	Processes  []string `json:"processes,omitempty"`
	URLs       []string `json:"urls"`
	Format     string   `json:"format"`
}

// Options configures a Catalog.
type Options struct {
	// DataDir persists the last successful refresh to DataDir/games.json
	// (atomic temp+rename write); empty disables caching.
	DataDir string
	// Sources are refreshed in order on every Refresh; nil means
	// DefaultSources().
	Sources []Source
}

// DefaultSources returns the refresh sources matching the builtin snapshot:
// Valve matchmaking ranges for cs2/dota2, Riot's AS6507 for lol/valorant,
// EA AS17260 for apex, Blizzard AS39651, Ubisoft AS39414, Facepunch AS43515,
// and G-Core AS199524 for standoff2.
func DefaultSources() []Source {
	valve := []string{
		"https://raw.githubusercontent.com/denniskupec/valve-matchmaking-ip-ranges/master/old%20list.md",
		"https://raw.githubusercontent.com/ryad-lindner/csgo-matchmaking-ip-ranges/master/README.md",
	}
	riot := []string{
		"https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS6507",
	}
	ea := []string{
		"https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS17260",
	}
	blizzard := []string{
		"https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS39651",
	}
	ubisoft := []string{
		"https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS39414",
	}
	facepunch := []string{
		"https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS43515",
	}
	gcore := []string{
		"https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS199524",
	}
	pubg := []string{
		"https://raw.githubusercontent.com/FQrabbit/SSTap-Rule/master/rules/PUBG-INT.rules",
	}
	return []Source{
		{Name: "cs2", GameName: "Counter-Strike 2", GameNameRu: "Counter-Strike 2", Processes: []string{"cs2.exe", "cs2"}, URLs: valve, Format: FormatPlain},
		{Name: "dota2", GameName: "Dota 2", GameNameRu: "Dota 2", Processes: []string{"dota2.exe", "dota2"}, URLs: valve, Format: FormatPlain},
		{Name: "lol", GameName: "League of Legends", GameNameRu: "League of Legends", Processes: []string{"LeagueClient.exe", "League of Legends.exe"}, URLs: riot, Format: FormatRIPEstat},
		{Name: "valorant", GameName: "Valorant", GameNameRu: "Valorant", Processes: []string{"VALORANT-Win64-Shipping.exe", "VALORANT.exe", "RiotClientServices.exe"}, URLs: riot, Format: FormatRIPEstat},
		{Name: "apex", GameName: "Apex Legends", GameNameRu: "Apex Legends", Processes: []string{"r5apex.exe", "r5apex_dx12.exe"}, URLs: ea, Format: FormatRIPEstat},
		{Name: "blizzard", GameName: "Blizzard / Overwatch 2", GameNameRu: "Blizzard / Overwatch 2", Processes: []string{"Overwatch.exe", "Battle.net.exe"}, URLs: blizzard, Format: FormatRIPEstat},
		{Name: "pubg", GameName: "PUBG: Battlegrounds", GameNameRu: "PUBG: Battlegrounds", Processes: []string{"TslGame.exe", "TslGame_UC.exe"}, URLs: pubg, Format: FormatPlain},
		{Name: "r6", GameName: "Rainbow Six Siege", GameNameRu: "Rainbow Six Siege", Processes: []string{"RainbowSix.exe", "RainbowSix_Vulkan.exe"}, URLs: ubisoft, Format: FormatRIPEstat},
		{Name: "rust", GameName: "Rust", GameNameRu: "Rust", Processes: []string{"RustClient.exe"}, URLs: facepunch, Format: FormatRIPEstat},
		{Name: "standoff2", GameName: "Standoff 2", GameNameRu: "Standoff 2", Processes: []string{"Standoff2.exe", "com.axlebolt.standoff2"}, URLs: gcore, Format: FormatRIPEstat},
	}
}

// Catalog holds the current game ranges. It starts from the builtin snapshot
// (optionally replaced by a newer on-disk cache) and refreshes from the
// configured sources; a failed refresh never empties it. Safe for concurrent
// use.
type Catalog struct {
	mu       sync.RWMutex
	games    []Game
	skipped  int
	opts     Options
	interval time.Duration // refresh period used by Run (refreshInterval by default)
}

// NewCatalog builds a catalog from the builtin snapshot, then tries to load
// fresher ranges from DataDir/games.json. A missing or broken cache is not
// an error — the snapshot stays.
func NewCatalog(opts Options) *Catalog {
	if opts.Sources == nil {
		opts.Sources = DefaultSources()
	}
	c := &Catalog{opts: opts, interval: refreshInterval}
	c.games = builtinGames()
	if opts.DataDir != "" {
		if cached, ok := loadCache(opts.DataDir); ok {
			c.games = cached
		}
	}
	return c
}

// List returns the catalog for listings: metadata and range counts, without
// the CIDR arrays (keeps GET /api/games responses small).
func (c *Catalog) List() []Game {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Game, len(c.games))
	for i, g := range c.games {
		out[i] = g
		out[i].CIDRs = nil
	}
	return out
}

// Routes returns copies of the CIDR ranges of the named games; unknown ids
// are silently skipped. The result is never nil.
func (c *Catalog) Routes(ids []string) map[string][]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string][]string, len(ids))
	for _, id := range ids {
		for _, g := range c.games {
			if g.ID != id {
				continue
			}
			cidrs := make([]string, len(g.CIDRs))
			copy(cidrs, g.CIDRs)
			out[id] = cidrs
		}
	}
	return out
}

// Processes returns deduplicated executable process names for the named games
// (used by WireSock and TunnlTo split-tunneling configs).
func (c *Catalog) Processes(ids []string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := make(map[string]bool)
	var out []string
	for _, id := range ids {
		for _, g := range c.games {
			if g.ID != id {
				continue
			}
			for _, p := range g.Processes {
				p = strings.TrimSpace(p)
				if p != "" && !seen[p] {
					seen[p] = true
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// Skipped returns how many junk lines were discarded while parsing sources
// since startup — a diagnostic for "an upstream list changed format".
func (c *Catalog) Skipped() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.skipped
}

// Refresh fetches fresh ranges for every configured source and atomically
// replaces the catalog. URLs of one source are tried in order; a failed or
// empty source fails over to the next URL. If any source exhausts its URLs
// the catalog is left untouched and an error is returned.
func (c *Catalog) Refresh(ctx context.Context) error {
	built := make([]Game, 0, len(c.opts.Sources))
	skipped := 0
	for _, src := range c.opts.Sources {
		g, s, err := refreshGame(ctx, src)
		if err != nil {
			return err
		}
		built = append(built, g)
		skipped += s
	}
	c.mu.Lock()
	c.games = built
	c.skipped += skipped
	c.mu.Unlock()
	if c.opts.DataDir != "" {
		if err := saveCache(c.opts.DataDir, built); err != nil {
			slog.Default().Warn("games: cache write failed", "dir", c.opts.DataDir, "error", err)
		}
	}
	return nil
}

// Run blocks until ctx is cancelled, refreshing the catalog immediately and
// then every 24 hours. A failed refresh keeps the previous catalog and is
// retried on the next tick.
func (c *Catalog) Run(ctx context.Context) {
	if err := c.Refresh(ctx); err != nil {
		slog.Default().Warn("games: initial refresh failed, serving snapshot/cache", "error", err)
	}
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.Refresh(ctx); err != nil {
				slog.Default().Warn("games: refresh failed, keeping previous catalog", "error", err)
			}
		}
	}
}

func refreshGame(ctx context.Context, src Source) (Game, int, error) {
	if len(src.URLs) == 0 {
		return Game{}, 0, fmt.Errorf("game %q: no source URLs configured", src.Name)
	}
	var lastErr error
	for _, url := range src.URLs {
		body, err := fetch(ctx, url)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", url, err)
			continue
		}
		raw, skipped := parseSource(src.Format, body)
		cidrs := normalize(raw)
		if len(cidrs) == 0 {
			lastErr = fmt.Errorf("%s: no valid CIDRs in response", url)
			continue
		}
		return Game{
			ID:        src.Name,
			Name:      src.GameName,
			NameRu:    src.GameNameRu,
			Processes: src.Processes,
			CIDRs:     cidrs,
			CIDRCount: len(cidrs),
			Updated:   time.Now().UTC(),
		}, skipped, nil
	}
	return Game{}, 0, fmt.Errorf("game %q: all sources failed: %w", src.Name, lastErr)
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceSize))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

func parseSource(format string, body []byte) (cidrs []string, skipped int) {
	switch format {
	case FormatSSTap:
		return parseSSTap(body)
	case FormatRIPEstat:
		return parseRIPEstat(body)
	default:
		return parsePlain(body)
	}
}

// parsePlain extracts CIDRs from a plain-text list: one CIDR per line, with
// arbitrary junk tolerated. Markdown headers, prose and annotations yield no
// CIDR token and count as skipped lines; comment (#) and blank lines are
// ignored outright.
func parsePlain(body []byte) ([]string, int) {
	var cidrs []string
	skipped := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(cutComment(line))
		if line == "" {
			continue
		}
		if !takeCIDRs(&cidrs, line) {
			skipped++
		}
	}
	return cidrs, skipped
}

// parseSSTap extracts CIDRs from an SSTap-Rule file: the same line
// discipline as plain, plus [section] headers which are structure rather
// than junk.
func parseSSTap(body []byte) ([]string, int) {
	var cidrs []string
	skipped := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(cutComment(line))
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		if !takeCIDRs(&cidrs, line) {
			skipped++
		}
	}
	return cidrs, skipped
}

// parseRIPEstat extracts prefixes from a RIPEstat announced-prefixes
// response: {"data":{"prefixes":[{"prefix":"..."},...]}}.
func parseRIPEstat(body []byte) ([]string, int) {
	var resp struct {
		Data struct {
			Prefixes []struct {
				Prefix string `json:"prefix"`
			} `json:"prefixes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0
	}
	var cidrs []string
	skipped := 0
	for _, p := range resp.Data.Prefixes {
		if _, _, err := net.ParseCIDR(p.Prefix); err == nil {
			cidrs = append(cidrs, p.Prefix)
		} else {
			skipped++
		}
	}
	return cidrs, skipped
}

func cutComment(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return line[:i]
	}
	return line
}

// takeCIDRs appends every whitespace-separated token of line that parses as
// a CIDR (handles trailing markdown annotations such as
// "146.66.156.0/24  (Stockholm, Sweden)") and reports whether any was found.
func takeCIDRs(cidrs *[]string, line string) bool {
	found := false
	for _, tok := range strings.Fields(line) {
		if _, _, err := net.ParseCIDR(tok); err == nil {
			*cidrs = append(*cidrs, tok)
			found = true
		}
	}
	return found
}

// normalize validates, canonicalises, de-duplicates, bogon-filters and
// numerically sorts a raw CIDR list (IPv4 before IPv6, shorter prefixes
// first on equal network address).
func normalize(cidrs []string) []string {
	type entry struct {
		net *net.IPNet
		key string
	}
	seen := make(map[string]struct{}, len(cidrs))
	var out []entry
	for _, s := range cidrs {
		_, ipnet, err := net.ParseCIDR(s)
		if err != nil || isBogon(ipnet) {
			continue
		}
		key := ipnet.String() // canonical network/prefix form
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, entry{net: ipnet, key: key})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].net, out[j].net
		if cmp := bytes.Compare(a.IP.To16(), b.IP.To16()); cmp != 0 {
			return cmp < 0
		}
		ai, _ := a.Mask.Size()
		bi, _ := b.Mask.Size()
		return ai < bi
	})
	res := make([]string, len(out))
	for i, e := range out {
		res[i] = e.key
	}
	return res
}

// bogonSpecs are special-purpose ranges that must never end up in a game
// tunnel: RFC1918 and friends, shared/loopback/link-local, benchmarking,
// documentation and multicast space, for both address families.
var bogonSpecs = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
	"192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
	"224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8",
}

var bogonNets = func() []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(bogonSpecs))
	for _, s := range bogonSpecs {
		if _, ipnet, err := net.ParseCIDR(s); err == nil {
			nets = append(nets, ipnet)
		}
	}
	return nets
}()

// isBogon reports whether n overlaps any special-purpose range in either
// direction: a subnet of a bogon, or a supernet covering one (0.0.0.0/0
// would swallow the whole internet into the tunnel — reject it too).
func isBogon(n *net.IPNet) bool {
	for _, b := range bogonNets {
		if b.Contains(n.IP) || n.Contains(b.IP) {
			return true
		}
	}
	return false
}

// saveCache persists games to dir/games.json atomically (temp file + rename
// on the same filesystem).
func saveCache(dir string, games []Game) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(games, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".games-*.tmp")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(name, filepath.Join(dir, cacheFile)); err != nil {
		os.Remove(name)
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}

// loadCache reads dir/games.json; any problem (missing, malformed, no
// usable games) means "not usable" and the caller keeps the snapshot.
func loadCache(dir string) ([]Game, bool) {
	data, err := os.ReadFile(filepath.Join(dir, cacheFile))
	if err != nil {
		return nil, false
	}
	var games []Game
	if err := json.Unmarshal(data, &games); err != nil {
		return nil, false
	}
	out := make([]Game, 0, len(games))
	for _, g := range games {
		if g.ID == "" {
			continue
		}
		g.CIDRs = normalize(g.CIDRs)
		if len(g.CIDRs) == 0 {
			continue
		}
		g.CIDRCount = len(g.CIDRs)
		out = append(out, g)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}
