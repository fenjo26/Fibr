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

// TestRelayRouting: with FIBR_RELAY_HOST set, configs and links point at the
// relay by default (FIBR_RELAY_DEFAULT) and at the server with ?via=direct;
// VLESS keeps the real domain as SNI/Host so TLS stays end-to-end.
func TestRelayRouting(t *testing.T) {
	a, st := newTestAPI(t, true)
	a.cfg.XHTTPEnabled, a.cfg.XHTTPHost, a.cfg.XHTTPPath = true, "vpn.example.com", "/sec"
	a.cfg.AWGEnabled, a.cfg.AWGPort, a.cfg.AWGInterface = true, 51821, "awg0"
	a.cfg.AWGSubnet, a.cfg.AWGSubnet6, a.cfg.Subnet6 = "10.9.0.0/24", "fd42:f1b3::/64", "fd42:f1b2::/64"
	a.cfg.AWGVersion = 2
	m, err := wg.NewAWG(a.cfg, st, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	a.SetAWG(m)

	u := &store.User{Username: "oleg"}
	if err := st.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	tok := a.newSessionToken(u.ID)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}
	ids := map[string]int64{}
	for _, p := range []string{"wg", "xray", "awg"} {
		rec := do(http.MethodPost, "/api/me/devices", `{"name":"`+p+`","protocol":"`+p+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", p, rec.Code, rec.Body.String())
		}
		var d deviceOut
		_ = json.Unmarshal(rec.Body.Bytes(), &d)
		ids[p] = d.ID
	}
	get := func(p, q string) string {
		rec := do(http.MethodGet, "/api/me/devices/"+itoa(ids[p])+"/config"+q, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("config %s%s: %d %s", p, q, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	// No relay configured: ?via=relay is ignored.
	if c := get("wg", "?via=relay"); !strings.Contains(c, "Endpoint = vpn.example.com:51820") {
		t.Fatalf("no relay, wg: %s", c)
	}

	a.cfg.RelayHost, a.cfg.RelayDefault = "203.0.113.50", true
	if c := get("wg", ""); !strings.Contains(c, "Endpoint = 203.0.113.50:51820") {
		t.Errorf("default via relay, wg:\n%s", c)
	}
	if c := get("awg", ""); !strings.Contains(c, "Endpoint = 203.0.113.50:51821") || !strings.Contains(c, "S3 = ") {
		t.Errorf("default via relay, awg:\n%s", c)
	}
	link := get("xray", "")
	if !strings.Contains(link, "@203.0.113.50:443?") || !strings.Contains(link, "sni=vpn.example.com") || !strings.Contains(link, "host=vpn.example.com") || !strings.Contains(link, "type=xhttp") {
		t.Errorf("default via relay, vless: %s", link)
	}
	if c := get("wg", "?via=direct"); !strings.Contains(c, "Endpoint = vpn.example.com:51820") {
		t.Errorf("direct wg:\n%s", c)
	}
	if l := get("xray", "?via=direct"); !strings.Contains(l, "@vpn.example.com:443?") {
		t.Errorf("direct vless: %s", l)
	}

	// Default off: direct unless asked.
	a.cfg.RelayDefault = false
	if c := get("wg", ""); !strings.Contains(c, "vpn.example.com:51820") {
		t.Errorf("default off, wg:\n%s", c)
	}
	if c := get("wg", "?via=relay"); !strings.Contains(c, "203.0.113.50:51820") {
		t.Errorf("default off + via=relay, wg:\n%s", c)
	}

	// /api/me tells the client a relay exists.
	if b := do(http.MethodGet, "/api/me", "").Body.String(); !strings.Contains(b, `"relay_enabled":true`) || !strings.Contains(b, `"relay_default":false`) {
		t.Errorf("/api/me: %s", b)
	}
	// QR follows the same routing (just check it renders).
	if rec := do(http.MethodGet, "/api/me/devices/"+itoa(ids["xray"])+"/qr.png?via=relay", ""); rec.Code != http.StatusOK {
		t.Errorf("qr: %d", rec.Code)
	}
}
