package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fenjo26/Fibr/internal/store"
	"github.com/fenjo26/Fibr/internal/wg"
)

// TestAWGProtocol: "awg" devices live on the second tunnel — own subnet,
// AmneziaWG keys in the config, the AWG port — and are refused while the
// tunnel is off.
func TestAWGProtocol(t *testing.T) {
	a, st := newTestAPI(t, false)
	u := &store.User{Username: "alice"}
	if err := st.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	tok := a.newSessionToken(u.ID)
	create := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/me/devices", strings.NewReader(`{"name":"amnezia","protocol":"awg"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := create(); rec.Code != http.StatusBadRequest {
		t.Fatalf("awg while off: %d %s", rec.Code, rec.Body.String())
	}

	a.cfg.AWGEnabled = true
	a.cfg.AWGPort = 51821
	a.cfg.AWGInterface = "awg0"
	a.cfg.AWGSubnet = "10.9.0.0/24"
	a.cfg.AWGSubnet6 = "fd42:f1b3::/64"
	a.cfg.Subnet6 = "fd42:f1b2::/64"
	m, err := wg.NewAWG(a.cfg, st, slog.Default())
	if err != nil {
		t.Fatalf("awg manager: %v", err)
	}
	a.SetAWG(m)

	rec := create()
	if rec.Code != http.StatusCreated {
		t.Fatalf("awg create: %d %s", rec.Code, rec.Body.String())
	}
	var d deviceOut
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Protocol != "awg" || !strings.HasPrefix(d.IPv4, "10.9.0.") {
		t.Fatalf("awg device: %+v", d)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/me/devices/"+itoa(d.ID)+"/config", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	cfg := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(cfg, "Jc = ") || !strings.Contains(cfg, ":51821") || !strings.Contains(cfg, "fd42:f1b3::") {
		t.Fatalf("awg config: %d\n%s", rec.Code, cfg)
	}
	// /api/me advertises it.
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"awg_enabled":true`) {
		t.Fatalf("/api/me: %s", rec.Body.String())
	}
	// A plain wg device keeps the main subnet and a plain config.
	req = httptest.NewRequest(http.MethodPost, "/api/me/devices", strings.NewReader(`{"name":"phone"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	var w deviceOut
	_ = json.Unmarshal(rec.Body.Bytes(), &w)
	if w.Protocol != "wg" || !strings.HasPrefix(w.IPv4, "10.8.0.") {
		t.Fatalf("wg device: %+v", w)
	}
}
