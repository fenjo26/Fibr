package games

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestParsePlain(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		want        []string
		wantSkipped int
	}{
		{
			name:        "clean lines",
			in:          "9.9.9.0/24\n147.12.0.0/16\n",
			want:        []string{"9.9.9.0/24", "147.12.0.0/16"},
			wantSkipped: 0,
		},
		{
			name: "markdown list with headers and annotations",
			in: "# CSGO Valve Matchmaking IP Ranges\n" +
				"**EU North**\n" +
				"146.66.156.0/24  (Stockholm, Sweden)\n" +
				"185.25.180.0/24\n" +
				"Send a pull request if something is not correct.\n",
			want:        []string{"146.66.156.0/24", "185.25.180.0/24"},
			wantSkipped: 2, // **EU North** + the prose line; the # header is a comment
		},
		{
			name:        "inline comments",
			in:          "103.10.125.0/24 # Sydney\n# full-line comment\n",
			want:        []string{"103.10.125.0/24"},
			wantSkipped: 0,
		},
		{
			name:        "garbage and broken cidrs",
			in:          "not a cidr\n999.999.0.0/24\n146.66\n\n   \n",
			want:        nil,
			wantSkipped: 3, // blank/whitespace lines are ignored, not counted
		},
		{
			name:        "crlf line endings",
			in:          "9.9.9.0/24\r\n147.12.0.0/16\r\n",
			want:        []string{"9.9.9.0/24", "147.12.0.0/16"},
			wantSkipped: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, skipped := parsePlain([]byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("cidrs = %v, want %v", got, tc.want)
			}
			if skipped != tc.wantSkipped {
				t.Fatalf("skipped = %d, want %d", skipped, tc.wantSkipped)
			}
		})
	}
}

func TestParseSSTap(t *testing.T) {
	in := "# SSTap-Rule\n" +
		"# https://github.com/FQrabbit/SSTap-Rule\n" +
		"[CORE]\n" +
		"Raw = True\n" +
		"[RULE]\n" +
		"9.9.9.0/24\n" +
		"147.12.0.0/16 # inline\n" +
		"\n"
	got, skipped := parseSSTap([]byte(in))
	want := []string{"9.9.9.0/24", "147.12.0.0/16"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cidrs = %v, want %v", got, want)
	}
	// [CORE]/[RULE] are structure; only the "Raw = True" line is junk.
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
}

func TestParseRIPEstat(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		in := `{"data":{"resource":"AS6507","prefixes":[` +
			`{"prefix":"9.9.9.0/24"},{"prefix":"147.12.0.0/16"},{"prefix":"AS6507"}]}}`
		got, skipped := parseRIPEstat([]byte(in))
		want := []string{"9.9.9.0/24", "147.12.0.0/16"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("cidrs = %v, want %v", got, want)
		}
		if skipped != 1 {
			t.Fatalf("skipped = %d, want 1", skipped)
		}
	})
	t.Run("malformed json yields nothing", func(t *testing.T) {
		got, skipped := parseRIPEstat([]byte(`{"data":`))
		if got != nil || skipped != 0 {
			t.Fatalf("got %v, skipped %d; want nil, 0", got, skipped)
		}
	})
}

func TestNormalize(t *testing.T) {
	in := []string{
		"146.66.155.0/24",
		"9.9.9.0/24",
		"9.9.9.0/24",      // duplicate
		"146.66.155.7/24", // non-canonical host bits -> canonicalises onto the first
		"10.0.0.0/8",      // RFC1918
		"192.168.1.0/24",  // RFC1918
		"100.100.0.0/16",  // CGNAT shared space
		"127.0.0.0/8",     // loopback
		"224.0.0.0/4",     // multicast
		"0.0.0.0/0",       // the whole internet
		"fe80::/10",       // link-local v6
		"fc00::/7",        // ULA
		"2a04:82c0::/29",  // legit global v6
	}
	want := []string{"9.9.9.0/24", "146.66.155.0/24", "2a04:82c0::/29"}
	got := normalize(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %v, want %v", got, want)
	}
}

func TestBuiltinSnapshot(t *testing.T) {
	wantCounts := map[string]int{
		"cs2": 47, "dota2": 47, "lol": 15, "valorant": 15,
		"apex": 3, "blizzard": 6, "pubg": 3, "r6": 3, "rust": 3, "standoff2": 3,
	}
	if len(builtinSnapshot) != len(wantCounts) {
		t.Fatalf("snapshot has %d games, want %d", len(builtinSnapshot), len(wantCounts))
	}
	cat := NewCatalog(Options{}) // no datadir -> snapshot verbatim
	for _, g := range cat.List() {
		want, ok := wantCounts[g.ID]
		if !ok {
			t.Fatalf("unexpected game id %q", g.ID)
		}
		if g.CIDRCount != want {
			t.Errorf("%s: cidr_count = %d, want %d", g.ID, g.CIDRCount, want)
		}
		if g.Updated != snapshotDate {
			t.Errorf("%s: updated = %v, want %v", g.ID, g.Updated, snapshotDate)
		}
		if g.Name == "" || g.NameRu == "" {
			t.Errorf("%s: missing names (%q/%q)", g.ID, g.Name, g.NameRu)
		}
	}
	for id := range wantCounts {
		cidrs := cat.Routes([]string{id})[id]
		for _, c := range cidrs {
			_, ipnet, err := net.ParseCIDR(c)
			if err != nil {
				t.Errorf("%s: invalid cidr %q", id, c)
				continue
			}
			if isBogon(ipnet) {
				t.Errorf("%s: bogon in snapshot: %q", id, c)
			}
		}
		// The snapshot must already be in canonical form (normalized once in
		// builtinGames, so a second pass is a no-op).
		if again := normalize(cidrs); !reflect.DeepEqual(again, cidrs) {
			t.Errorf("%s: snapshot not canonical: %v vs %v", id, cidrs, again)
		}
	}
}

func TestListHidesCIDRsAndJSONShape(t *testing.T) {
	cat := NewCatalog(Options{})
	list := cat.List()
	if len(list) == 0 {
		t.Fatal("empty listing")
	}
	for _, g := range list {
		if g.CIDRs != nil {
			t.Errorf("%s: listing leaks cidrs", g.ID)
		}
		if g.CIDRCount <= 0 {
			t.Errorf("%s: cidr_count not visible: %d", g.ID, g.CIDRCount)
		}
		if g.Updated.IsZero() {
			t.Errorf("%s: updated not visible", g.ID)
		}
	}

	data, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, row := range rows {
		if _, ok := row["cidrs"]; ok {
			t.Errorf("listing json has cidrs key: %v", row)
		}
		for _, key := range []string{"id", "name", "name_ru", "cidr_count", "updated"} {
			if _, ok := row[key]; !ok {
				t.Errorf("listing json missing %q: %v", key, row)
			}
		}
	}
}

func TestRoutes(t *testing.T) {
	cat := NewCatalog(Options{})

	r := cat.Routes([]string{"cs2", "unknown-game", ""})
	if len(r) != 1 {
		t.Fatalf("routes = %v, want only cs2", r)
	}
	cs2 := r["cs2"]
	if len(cs2) == 0 {
		t.Fatal("cs2 routes empty")
	}

	// Returned slices are copies: mutating them must not touch the catalog.
	cs2[0] = "198.51.100.0/24"
	again := cat.Routes([]string{"cs2"})["cs2"]
	if again[0] == cs2[0] {
		t.Fatalf("routes are not copies: %v", again)
	}
	if !reflect.DeepEqual(again, cat.Routes([]string{"cs2"})["cs2"]) {
		t.Fatal("routes unstable across calls")
	}

	// Unknown ids and empty input return an empty, non-nil map.
	if got := cat.Routes(nil); got == nil || len(got) != 0 {
		t.Fatalf("Routes(nil) = %v, want empty non-nil map", got)
	}
	if got := cat.Routes([]string{"nope"}); got == nil || len(got) != 0 {
		t.Fatalf("Routes(unknown) = %v, want empty non-nil map", got)
	}
}

func TestCacheRoundtrip(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("9.9.9.0/24\n147.12.0.0/16\n"))
	}))
	defer srv.Close()

	opts := Options{
		DataDir: dir,
		Sources: []Source{{Name: "cs2", GameName: "Counter-Strike 2", GameNameRu: "Counter-Strike 2",
			URLs: []string{srv.URL}, Format: FormatPlain}},
	}
	cat := NewCatalog(opts)
	if err := cat.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, cacheFile)); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	// A second catalog with a dead upstream must boot from the cache and
	// serve the refreshed ranges and timestamp, not the snapshot.
	srv.Close()
	cat2 := NewCatalog(opts)
	r := cat2.Routes([]string{"cs2"})["cs2"]
	want := []string{"9.9.9.0/24", "147.12.0.0/16"}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("cache roundtrip = %v, want %v", r, want)
	}
	if got, wantTS := cat2.List()[0].Updated, cat.List()[0].Updated; !got.Equal(wantTS) {
		t.Fatalf("cache lost updated timestamp: %v vs %v", got, wantTS)
	}
}

func TestCacheBrokenFallsBackToSnapshot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cacheFile), []byte("}not json{"), 0o644); err != nil {
		t.Fatalf("write bad cache: %v", err)
	}
	cat := NewCatalog(Options{DataDir: dir})
	if r := cat.Routes([]string{"cs2"})["cs2"]; len(r) != 47 {
		t.Fatalf("broken cache did not fall back to snapshot: %d cidrs", len(r))
	}
}

func TestRefreshReplacesCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("9.9.9.0/24\n147.12.0.0/16\n"))
	}))
	defer srv.Close()

	cat := NewCatalog(Options{
		DataDir: t.TempDir(),
		Sources: []Source{{Name: "cs2", GameName: "Counter-Strike 2", GameNameRu: "Counter-Strike 2",
			URLs: []string{srv.URL}, Format: FormatPlain}},
	})
	if err := cat.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	list := cat.List()
	if len(list) != 1 || list[0].ID != "cs2" || list[0].CIDRCount != 2 {
		t.Fatalf("catalog not replaced: %+v", list)
	}
	// The refresh covered only the configured source: the other snapshot
	// games are gone.
	if r := cat.Routes([]string{"dota2", "lol"}); len(r) != 0 {
		t.Fatalf("stale games survived refresh: %v", r)
	}
	if cat.Skipped() != 0 {
		t.Fatalf("skipped = %d, want 0", cat.Skipped())
	}
}

func TestRefreshFailover(t *testing.T) {
	var firstHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		firstHits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("# list\n9.9.9.0/24\njunk line\n147.12.0.0/16\n"))
	}))
	defer second.Close()

	cat := NewCatalog(Options{
		DataDir: t.TempDir(),
		Sources: []Source{{Name: "cs2", GameName: "Counter-Strike 2", GameNameRu: "Counter-Strike 2",
			URLs: []string{first.URL, second.URL}, Format: FormatPlain}},
	})
	if err := cat.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh with failover: %v", err)
	}
	if firstHits.Load() == 0 {
		t.Fatal("first url was not tried")
	}
	r := cat.Routes([]string{"cs2"})["cs2"]
	want := []string{"9.9.9.0/24", "147.12.0.0/16"}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("routes after failover = %v, want %v", r, want)
	}
	if cat.Skipped() != 1 { // the junk line in the winning source
		t.Fatalf("skipped = %d, want 1", cat.Skipped())
	}
}

func TestRefreshAllFailedKeepsCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Run("all urls down", func(t *testing.T) {
		dir := t.TempDir()
		cat := NewCatalog(Options{
			DataDir: dir,
			Sources: []Source{{Name: "cs2", URLs: []string{srv.URL, srv.URL}, Format: FormatPlain}},
		})
		before := cat.List()
		if err := cat.Refresh(context.Background()); err == nil {
			t.Fatal("expected error when all sources fail")
		}
		if after := cat.List(); !reflect.DeepEqual(before, after) {
			t.Fatal("catalog mutated by a failed refresh")
		}
		// No cache may be written for a failed refresh.
		if _, err := os.Stat(filepath.Join(dir, cacheFile)); !os.IsNotExist(err) {
			t.Fatalf("failed refresh wrote a cache: %v", err)
		}
	})

	t.Run("no urls configured", func(t *testing.T) {
		cat := NewCatalog(Options{
			DataDir: t.TempDir(),
			Sources: []Source{{Name: "cs2", Format: FormatPlain}},
		})
		if err := cat.Refresh(context.Background()); err == nil {
			t.Fatal("expected error for a source without urls")
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		cat := NewCatalog(Options{Sources: []Source{{Name: "cs2", URLs: []string{srv.URL}}}})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		before := cat.List()
		if err := cat.Refresh(ctx); err == nil {
			t.Fatal("expected error on cancelled context")
		}
		if after := cat.List(); !reflect.DeepEqual(before, after) {
			t.Fatal("catalog mutated by a cancelled refresh")
		}
	})
}

func TestRun(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write([]byte("9.9.9.0/24\n"))
	}))
	defer srv.Close()

	cat := NewCatalog(Options{
		DataDir: t.TempDir(),
		Sources: []Source{{Name: "cs2", GameName: "Counter-Strike 2", GameNameRu: "Counter-Strike 2",
			URLs: []string{srv.URL}, Format: FormatPlain}},
	})
	cat.interval = 20 * time.Millisecond // shrink the 24h period for the test

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { cat.Run(ctx); close(done) }()

	// Initial refresh plus at least two ticks.
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := hits.Load(); n < 3 {
		t.Fatalf("run performed %d refreshes, want >= 3", n)
	}
	if r := cat.Routes([]string{"cs2"})["cs2"]; len(r) != 1 || r[0] != "9.9.9.0/24" {
		t.Fatalf("catalog not refreshed by Run: %v", r)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestCatalogProcesses(t *testing.T) {
	cat := NewCatalog(Options{})
	procs := cat.Processes([]string{"cs2", "apex", "unknown"})
	want := []string{"cs2.exe", "cs2", "r5apex.exe", "r5apex_dx12.exe"}
	if !reflect.DeepEqual(procs, want) {
		t.Fatalf("processes = %v, want %v", procs, want)
	}
}
