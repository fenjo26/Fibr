package dns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// listServer serves mutable list bodies with ETag revalidation and counts
// requests that actually returned a body (304s are counted separately).
type listServer struct {
	*httptest.Server
	body        atomic.Value // string
	fetches     atomic.Int32
	notModified atomic.Int32
}

func newListServer(initial string) *listServer {
	ls := &listServer{}
	ls.body.Store(initial)
	ls.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		etag := `"len-` + strconv.Itoa(len(ls.body.Load().(string))) + `"`
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			ls.notModified.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		ls.fetches.Add(1)
		_, _ = w.Write([]byte(ls.body.Load().(string)))
	}))
	return ls
}

func (ls *listServer) setBody(s string) { ls.body.Store(s) }

func newTestManager(t *testing.T, adblockURL, adultURL string) *ListManager {
	t.Helper()
	m, err := NewListManager(t.TempDir(), NewFilter(), adblockURL, adultURL, nil)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func TestListsETagCycle(t *testing.T) {
	srv := newListServer("0.0.0.0 first.example\n")
	defer srv.Close()

	m := newTestManager(t, srv.URL, srv.URL+"/adult")
	ctx := context.Background()

	// First refresh downloads both lists.
	if err := m.RefreshAll(ctx); err != nil {
		t.Fatalf("refresh #1: %v", err)
	}
	if got := srv.fetches.Load(); got != 2 {
		t.Fatalf("expected 2 real downloads (adblock+adult), got %d", got)
	}
	lens := m.filter.SetsLen()
	if lens[SetAdblock] == 0 || lens[SetAdult] == 0 {
		t.Fatalf("sets not loaded: %v", lens)
	}
	if ok, _ := m.filter.Match([]string{SetAdblock}, "first.example"); !ok {
		t.Fatal("rule from downloaded list must match")
	}

	// Second refresh: ETag matches -> 304, no body transfer.
	if err := m.RefreshAll(ctx); err != nil {
		t.Fatalf("refresh #2: %v", err)
	}
	if got := srv.fetches.Load(); got != 2 {
		t.Fatalf("etag must prevent re-download, got %d fetches", got)
	}
	if got := srv.notModified.Load(); got != 2 {
		t.Fatalf("expected 2 not-modified responses, got %d", got)
	}

	// Content changes -> new body, new rules.
	srv.setBody("||second.example^\n0.0.0.0 first.example\n")
	if err := m.RefreshAll(ctx); err != nil {
		t.Fatalf("refresh #3: %v", err)
	}
	if ok, _ := m.filter.Match([]string{SetAdblock}, "second.example"); !ok {
		t.Fatal("new rule must be active after refresh")
	}

	// last_ok/last_check are recorded.
	for _, e := range m.Lists() {
		if !e.BuiltIn {
			continue
		}
		if e.LastOK.IsZero() || e.LastCheck.IsZero() {
			t.Errorf("list %s: last_ok/last_check not recorded", e.Name)
		}
		if time.Since(e.LastOK) > time.Minute {
			t.Errorf("list %s: stale last_ok %v", e.Name, e.LastOK)
		}
	}
}

func TestListsCRUD(t *testing.T) {
	srv := newListServer("0.0.0.0 ads.example\n0.0.0.0 more.example\n")
	defer srv.Close()
	customSrv := newListServer("0.0.0.0 mine.example\n")
	defer customSrv.Close()

	m := newTestManager(t, srv.URL, srv.URL+"/adult")
	ctx := context.Background()
	if err := m.RefreshAll(ctx); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}

	// AddList downloads immediately and feeds the custom set.
	if err := m.AddList("mine", customSrv.URL); err != nil {
		t.Fatalf("add: %v", err)
	}
	if ok, set := m.filter.Match([]string{SetAdblock, SetCustom}, "mine.example"); !ok || set != SetCustom {
		t.Fatalf("custom list must match, ok=%v set=%s", ok, set)
	}
	if ok, set := m.filter.Match([]string{SetAdblock, SetCustom}, "more.example"); !ok || set != SetAdblock {
		t.Fatalf("builtin list must keep matching its own rules, ok=%v set=%s", ok, set)
	}

	// Validation.
	if err := m.AddList("bad", "ftp://example.invalid/list"); err == nil {
		t.Error("non-http(s) URL must be rejected")
	}
	if err := m.AddList("bad name!", srv.URL); err == nil {
		t.Error("invalid name must be rejected")
	}
	if err := m.AddList("dup", customSrv.URL); err == nil {
		t.Error("duplicate URL must be rejected")
	}

	// BuiltIn protection.
	if err := m.RemoveList(BuiltInAdblock); !errors.Is(err, ErrBuiltIn) {
		t.Errorf("removing builtin must fail with ErrBuiltIn, got %v", err)
	}

	// Disabling a list removes its rules; the cache makes re-enabling free.
	if err := m.SetListEnabled(BuiltInAdblock, false); err != nil {
		t.Fatalf("disable adblock: %v", err)
	}
	if _, present := m.filter.SetsLen()[SetAdblock]; present {
		t.Error("disabled builtin set must be dropped")
	}
	if err := m.SetListEnabled(BuiltInAdblock, true); err != nil {
		t.Fatalf("re-enable adblock: %v", err)
	}
	if _, present := m.filter.SetsLen()[SetAdblock]; !present {
		t.Error("re-enabled builtin set must be back (from cache)")
	}

	// Disabling the only custom list drops the custom set.
	if err := m.SetListEnabled("mine", false); err != nil {
		t.Fatalf("disable custom: %v", err)
	}
	if _, present := m.filter.SetsLen()[SetCustom]; present {
		t.Error("custom set must be dropped when no custom list is enabled")
	}

	// RemoveList deletes a custom list and its cache.
	if err := m.SetListEnabled("mine", true); err != nil {
		t.Fatalf("re-enable custom: %v", err)
	}
	if err := m.RemoveList("mine"); err != nil {
		t.Fatalf("remove custom: %v", err)
	}
	if _, present := m.filter.SetsLen()[SetCustom]; present {
		t.Error("custom set must be gone after removal")
	}
	if err := m.RemoveList("mine"); err == nil {
		t.Error("removing a missing list must fail")
	}

	// RefreshOne forces one list outside the ticker.
	if err := m.RefreshOne(ctx, BuiltInAdult); err != nil {
		t.Fatalf("refresh one: %v", err)
	}
	if err := m.RefreshOne(ctx, "nope"); err == nil {
		t.Error("refreshing a missing list must fail")
	}
}

func TestListsOfflineStartNotFatal(t *testing.T) {
	// A closed server: connection refused immediately.
	srv := newListServer("0.0.0.0 x.example\n")
	srv.Close()

	m := newTestManager(t, srv.URL+"/adblock", srv.URL+"/adult")
	// Constructing and refreshing against a dead endpoint is not fatal: the
	// manager comes up, the sets stay empty until the network returns.
	if lens := m.filter.SetsLen(); lens[SetAdblock] != 0 || lens[SetAdult] != 0 {
		t.Fatalf("no rules expected offline, got %v", lens)
	}
	if err := m.RefreshAll(context.Background()); err == nil {
		t.Error("refresh against a dead endpoint must report an error")
	}
	entries := m.Lists()
	if len(entries) != 2 {
		t.Fatalf("expected 2 builtin entries, got %d", len(entries))
	}
	for _, e := range entries {
		if !e.BuiltIn || !e.Enabled {
			t.Errorf("unexpected entry %+v", e)
		}
	}
}
