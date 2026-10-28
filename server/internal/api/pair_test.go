package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fenjo26/Fibr/internal/store"
)

// TestTVPairingFlow: TV gets a code, polls "waiting", the phone attaches a
// new device, the TV gets the config exactly once.
func TestTVPairingFlow(t *testing.T) {
	a, st := newTestAPI(t, true)
	u := &store.User{Username: "alice"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("user: %v", err)
	}
	tok := a.newSessionToken(u.ID)
	do := func(method, path, body string, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodPost, "/api/pair", "", false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create pairing: %d %s", rec.Code, rec.Body.String())
	}
	var p struct{ Code, Secret, Link, QR string }
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Code) != pairCodeLen || p.Secret == "" || !strings.Contains(p.Link, "pair="+p.Code) {
		t.Fatalf("bad pairing: %+v", p)
	}
	poll := "/api/pair/" + p.Code + "?secret=" + p.Secret

	if rec := do(http.MethodGet, "/api/pair/"+p.Code+"?secret=wrong", "", false); rec.Code != http.StatusNotFound {
		t.Fatalf("wrong secret: %d", rec.Code)
	}
	if rec := do(http.MethodGet, poll, "", false); !strings.Contains(rec.Body.String(), "waiting") {
		t.Fatalf("poll before attach: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/pair/"+p.Code+"/qr.png", "", false); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("qr: %d", rec.Code)
	}
	// Phone side: lower-case + "tv_" prefix are tolerated.
	if rec := do(http.MethodPost, "/api/me/pair/tv_"+strings.ToLower(p.Code), `{"name":"Living room"}`, false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated attach: %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/me/pair/tv_"+strings.ToLower(p.Code), `{"name":"Living room"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/api/me/pair/"+p.Code, `{}`, true); rec.Code != http.StatusConflict {
		t.Fatalf("second attach: %d", rec.Code)
	}
	rec = do(http.MethodGet, poll, "", false)
	var ready struct{ Status, Config string }
	_ = json.Unmarshal(rec.Body.Bytes(), &ready)
	if ready.Status != "ready" || !strings.Contains(ready.Config, "[Interface]") {
		t.Fatalf("poll after attach: %s", rec.Body.String())
	}
	if rec := do(http.MethodGet, poll, "", false); rec.Code != http.StatusNotFound {
		t.Fatalf("config must be handed out once, got %d", rec.Code)
	}
	devs, _ := st.ListDevices(u.ID)
	if len(devs) != 1 || devs[0].Name != "Living room" {
		t.Fatalf("devices after pairing: %+v", devs)
	}
}

func TestTVPairingExistingXrayRejected(t *testing.T) {
	a, st := newTestAPI(t, true)
	u := &store.User{Username: "bob"}
	if err := st.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	d, err := a.provisionDevice(u.ID, "phone", store.ProtocolXray)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/pair", nil))
	var p struct{ Code string }
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	req := httptest.NewRequest(http.MethodPost, "/api/me/pair/"+p.Code, strings.NewReader(`{"device_id":`+strconv.FormatInt(d.ID, 10)+`}`))
	req.Header.Set("Authorization", "Bearer "+a.newSessionToken(u.ID))
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("xray device for TV: %d %s", rec.Code, rec.Body.String())
	}
}

func TestConfFileName(t *testing.T) {
	cases := map[string]string{
		"iPhone":              "fibr-iPhone.conf",
		"Living room TV":      "fibr-Living-roo.conf",
		"a-very-long-name-xx": "fibr-a-very-lon.conf",
	}
	for in, want := range cases {
		if got := ConfFileName(in, "conf"); got != want {
			t.Errorf("ConfFileName(%q) = %q, want %q", in, got, want)
		}
	}
	// Non-latin names get a short stable hash instead of collapsing to
	// the same "fibr" tunnel name.
	a, b := ConfFileName("Телефон", "conf"), ConfFileName("Телевизор", "conf")
	if a == b || !strings.HasPrefix(a, "fibr-") || len(a) > 15+len(".conf") {
		t.Fatalf("non-latin names: %q %q", a, b)
	}
}

func TestStartLinks(t *testing.T) {
	if got := PairLink("vpn.example.com", "fibr_bot", "ABC234"); got != "https://t.me/fibr_bot?start=tv_ABC234" {
		t.Fatalf("PairLink with bot: %q", got)
	}
	if got := PairLink("vpn.example.com", "", "ABC234"); got != "https://vpn.example.com/miniapp/?pair=ABC234" {
		t.Fatalf("PairLink without bot: %q", got)
	}
}
