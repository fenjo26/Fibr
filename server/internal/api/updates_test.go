package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeUpdater stands in for the fibr-updater sidecar: a canned /status and a
// /update that records the call. Tests point a.updaterURL at srvURL.
type fakeUpdater struct {
	srvURL     string
	statusCode int    // /update answer
	statusBody string // /status answer
	calls      int    // POST /update count
}

func newFakeUpdater(t *testing.T, statusCode int, statusBody string) *fakeUpdater {
	t.Helper()
	f := &fakeUpdater{statusCode: statusCode, statusBody: statusBody}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(f.statusBody))
		case "/update":
			f.calls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(f.statusCode)
			_, _ = w.Write([]byte(`{"started":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	f.srvURL = srv.URL
	return f
}

// deadUpdater returns a server that is already closed — connection refused.
func deadUpdater(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	return srv.URL
}

// callUpdate hits the admin endpoints with the static admin token.
func callUpdates(t *testing.T, a *API, method, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+a.cfg.AdminToken)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("bad json from %s: %v: %s", path, err, rec.Body.String())
		}
	}
	return rec, body
}

func TestAdminUpdatesUpstreamDown(t *testing.T) {
	a, _ := newTestAPI(t, false)
	a.updaterURL = deadUpdater(t)

	rec, body := callUpdates(t, a, http.MethodGet, "/api/admin/updates")
	if rec.Code != http.StatusOK {
		t.Fatalf("status with updater down: %d", rec.Code)
	}
	if body["enabled"] != false {
		t.Fatalf("expected enabled=false, got %v", body["enabled"])
	}

	// Apply with the sidecar down must be a clean 503, not a panic/timeout.
	rec, body = callUpdates(t, a, http.MethodPost, "/api/admin/updates/apply")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("apply with updater down: %d", rec.Code)
	}
	if body["error"] != "updater not enabled" {
		t.Fatalf("apply error message: %v", body["error"])
	}
}

func TestAdminUpdatesUpstreamSlow(t *testing.T) {
	a, _ := newTestAPI(t, false)
	// A status handler slower than the (shrunken) client budget: the panel
	// must get enabled=false instead of hanging.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"current":"v1.0.0"}`))
	}))
	t.Cleanup(srv.Close)
	a.updaterURL = srv.URL

	old := updaterTimeout
	updaterTimeout = 50 * time.Millisecond
	t.Cleanup(func() { updaterTimeout = old })

	rec, body := callUpdates(t, a, http.MethodGet, "/api/admin/updates")
	if rec.Code != http.StatusOK || body["enabled"] != false {
		t.Fatalf("slow updater: code=%d body=%v", rec.Code, body)
	}
}

func TestAdminUpdatesUpstreamOK(t *testing.T) {
	a, _ := newTestAPI(t, false)
	f := newFakeUpdater(t, http.StatusAccepted,
		`{"current":"v1.2.0","current_sha":"aaa","remote_sha":"bbb","behind":true,"latest_release":"v1.3.0"}`)
	a.updaterURL = f.srvURL

	rec, body := callUpdates(t, a, http.MethodGet, "/api/admin/updates")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if body["enabled"] != true {
		t.Fatalf("expected enabled=true, got %v", body)
	}
	if body["current"] != "v1.2.0" || body["behind"] != true || body["latest_release"] != "v1.3.0" {
		t.Fatalf("status fields not proxied: %v", body)
	}
}

func TestAdminUpdatesApply(t *testing.T) {
	a, _ := newTestAPI(t, false)

	cases := []struct {
		name       string
		upstream   int
		wantCode   int
		wantErr    string
		wantCalled bool
	}{
		{name: "accepted", upstream: http.StatusAccepted, wantCode: http.StatusAccepted, wantCalled: true},
		{name: "conflict", upstream: http.StatusConflict, wantCode: http.StatusConflict, wantErr: "update already in progress", wantCalled: true},
		{name: "upstream broken", upstream: http.StatusInternalServerError, wantCode: http.StatusBadGateway, wantErr: "updater error", wantCalled: true},
	}
	for _, c := range cases {
		f := newFakeUpdater(t, c.upstream, `{"current":"v1.0.0"}`)
		a.updaterURL = f.srvURL
		rec, body := callUpdates(t, a, http.MethodPost, "/api/admin/updates/apply")
		if rec.Code != c.wantCode {
			t.Fatalf("%s: code=%d want %d", c.name, rec.Code, c.wantCode)
		}
		if c.wantErr != "" && body["error"] != c.wantErr {
			t.Fatalf("%s: error=%v want %q", c.name, body["error"], c.wantErr)
		}
		if f.calls != 1 {
			t.Fatalf("%s: updater called %d times, want 1", c.name, f.calls)
		}
	}
}

func TestAdminUpdatesRequireAdmin(t *testing.T) {
	a, _ := newTestAPI(t, false)
	for _, path := range []string{"/api/admin/updates", "/api/admin/updates/apply"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "apply") {
			method = http.MethodPost
		}
		req := httptest.NewRequest(method, path, nil) // no Authorization header
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s without token: %d, want 403", path, rec.Code)
		}
	}
}
