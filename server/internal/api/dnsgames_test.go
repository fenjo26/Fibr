package api

// Endpoints of the protected DNS and the game catalog (docs/dns-games.md
// §3): the public status probe, the owner's per-device profile switch and
// the public game catalog with routes.

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fenjo26/Fibr/internal/dns"
	"github.com/fenjo26/Fibr/internal/games"
	"github.com/fenjo26/Fibr/internal/store"
)

func TestDNSGamesEndpoints(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.DNSDefaultProfile = "adblock"

	// A resolver instance without Start(): the handlers only need the
	// pointer (listening is main's business).
	dcfg := dns.DefaultConfig()
	dcfg.DataDir = filepath.Join(t.TempDir(), "dns")
	srv, err := dns.NewServer(dcfg, slog.Default())
	if err != nil {
		t.Fatalf("dns server: %v", err)
	}
	t.Cleanup(srv.Stop)
	a.SetDNS(srv)
	a.SetGames(games.NewCatalog(games.Options{}))

	u := &store.User{Username: "carol"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("user: %v", err)
	}
	tok := a.newSessionToken(u.ID)
	auth := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var rd *strings.Reader
		if body == "" {
			rd = strings.NewReader("")
		} else {
			rd = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, rd)
		auth(req)
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}

	// Public status probe.
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dns/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dns status: %d %s", rec.Code, rec.Body.String())
	}
	var st1 struct {
		Enabled        bool   `json:"enabled"`
		DefaultProfile string `json:"default_profile"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st1); err != nil {
		t.Fatal(err)
	}
	if !st1.Enabled || st1.DefaultProfile != "adblock" {
		t.Fatalf("dns status out: %+v", st1)
	}

	// A wg device to switch the profile of.
	rec = call(http.MethodPost, "/api/me/devices", `{"name":"kids"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create device: %d %s", rec.Code, rec.Body.String())
	}

	// Profile with a schedule round-trips and marks the config stale.
	rec = call(http.MethodPut, "/api/me/devices/1/dns",
		`{"profile":"family","schedule":{"days":[1,2,3,4,5],"start":"07:00","end":"22:00","tz":"Europe/Berlin"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put dns: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Device      deviceOut `json:"device"`
		ConfigStale bool      `json:"config_stale"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Device.DNSProfile != "family" || !out.ConfigStale {
		t.Fatalf("put dns out: %+v", out)
	}
	// The rendered config now points DNS at the tunnel gateway (the wg
	// manager learns about the resolver through main; flip it like main
	// does after a successful Start).
	a.wg.SetDNSResolver(true)
	rec = call(http.MethodGet, "/api/me/devices/1/config", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "DNS = 10.8.0.1") {
		t.Fatalf("config after family: %d %s", rec.Code, rec.Body.String())
	}

	// "off" returns the public resolvers to the config.
	if rec := call(http.MethodPut, "/api/me/devices/1/dns", `{"profile":"off"}`); rec.Code != http.StatusOK {
		t.Fatalf("put off: %d", rec.Code)
	}
	rec = call(http.MethodGet, "/api/me/devices/1/config", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "DNS = 1.1.1.1") {
		t.Fatalf("config after off: %d %s", rec.Code, rec.Body.String())
	}

	// Personal never-block list round-trips; junk entries are rejected.
	rec = call(http.MethodPut, "/api/me/devices/1/dns", `{"profile":"adblock","allow":["Example.com.","shop.example.com"]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "example.com") {
		t.Fatalf("put allow: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(http.MethodPut, "/api/me/devices/1/dns", `{"profile":"adblock","allow":["https://x.com/y"]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad allow: %d", rec.Code)
	}
	// A later profile-only change keeps the stored exceptions.
	rec = call(http.MethodPut, "/api/me/devices/1/dns", `{"profile":"off"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "example.com") {
		t.Fatalf("allow must survive a profile change: %d %s", rec.Code, rec.Body.String())
	}

	// Game tunnel: config with the games' CIDRs instead of 0.0.0.0/0.
	rec = call(http.MethodGet, "/api/me/devices/1/game-config?games=cs2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("game config: %d %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, "AllowedIPs = 0.0.0.0/0, ::/0") {
		t.Fatalf("game config must not route everything:\n%s", body)
	}
	if rec := call(http.MethodGet, "/api/me/devices/1/game-config?games=", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("game config without games: %d", rec.Code)
	}

	// Invalid profile and invalid schedule are rejected.
	if rec := call(http.MethodPut, "/api/me/devices/1/dns", `{"profile":"strict"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid profile: %d", rec.Code)
	}
	if rec := call(http.MethodPut, "/api/me/devices/1/dns", `{"profile":"family","schedule":{"start":"25:99"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid schedule: %d", rec.Code)
	}

	// Game catalog: list and routes.
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/games", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("games: %d", rec.Code)
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 || list[0]["id"] == nil {
		t.Fatalf("games list: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/games/routes?ids=cs2", nil))
	var routes map[string][]string
	if err := json.Unmarshal(rec.Body.Bytes(), &routes); err != nil {
		t.Fatal(err)
	}
	if len(routes["cs2"]) == 0 {
		t.Fatalf("cs2 routes: %s", rec.Body.String())
	}

	// With the resolver off, the switch answers 503 and the public probe
	// reports disabled (the client hides the card).
	a.SetDNS(nil)
	if rec := call(http.MethodPut, "/api/me/devices/1/dns", `{"profile":"adblock"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("dns off: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/dns/status", nil))
	if !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("status disabled: %s", rec.Body.String())
	}
}
