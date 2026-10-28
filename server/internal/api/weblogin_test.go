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

// TestWebInviteSignup: an invite link alone creates the account (no
// Telegram), returns a working login key, and stays one-time.
func TestWebInviteSignup(t *testing.T) {
	a, st := newTestAPI(t, false)
	if err := st.CreateInvite(&store.Invite{Code: "WEBCODE1", Note: "Бабушка"}); err != nil {
		t.Fatal(err)
	}
	post := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return rec
	}
	if rec := post("/api/auth/invite", `{"invite_code":"NOPE"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("bad code: %d", rec.Code)
	}
	rec := post("/api/auth/invite", `{"invite_code":"WEBCODE1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ Token, Key, LoginLink string }
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	out.Token, _ = raw["token"].(string)
	out.Key, _ = raw["key"].(string)
	out.LoginLink, _ = raw["login_link"].(string)
	if out.Token == "" || len(out.Key) != accessKeyLen || out.LoginLink != "https://vpn.example.com/cabinet/#k="+out.Key {
		t.Fatalf("response: %s", rec.Body.String())
	}
	if u, _ := raw["user"].(map[string]any); u["username"] != "Бабушка" {
		t.Fatalf("username from invite note: %v", u["username"])
	}
	// The key signs in again (another device / the Android app).
	if rec := post("/api/auth/key", `{"key":"`+out.Key+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("key login: %d", rec.Code)
	}
	// One-time.
	if rec := post("/api/auth/invite", `{"invite_code":"WEBCODE1"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("reused invite: %d", rec.Code)
	}
	// The session works for creating a device.
	req := httptest.NewRequest(http.MethodPost, "/api/me/devices", strings.NewReader(`{"name":"phone"}`))
	req.Header.Set("Authorization", "Bearer "+out.Token)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("device with web session: %d %s", rec.Code, rec.Body.String())
	}
}

// TestLoginPassword: a signed-in person sets a login + password, signs in
// with it; wrong passwords fail and get rate-limited; logins are unique.
func TestLoginPassword(t *testing.T) {
	a, st := newTestAPI(t, false)
	u := &store.User{Username: "grandma"}
	other := &store.User{Username: "other"}
	if err := st.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(other); err != nil {
		t.Fatal(err)
	}
	do := func(path, body, tok string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}
	tok := a.newSessionToken(u.ID)
	if rec := do("/api/me/password", `{"login":"Ba","password":"12345678"}`, tok); rec.Code != http.StatusBadRequest {
		t.Fatalf("short login: %d", rec.Code)
	}
	if rec := do("/api/me/password", `{"login":"babushka","password":"123"}`, tok); rec.Code != http.StatusBadRequest {
		t.Fatalf("short password: %d", rec.Code)
	}
	if rec := do("/api/me/password", `{"login":"Babushka","password":"pirozhki2026"}`, tok); rec.Code != http.StatusOK {
		t.Fatalf("set: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do("/api/me/password", `{"login":"babushka","password":"whatever123"}`, a.newSessionToken(other.ID)); rec.Code != http.StatusConflict {
		t.Fatalf("taken login: %d", rec.Code)
	}
	if rec := do("/api/auth/password", `{"login":"BABUSHKA","password":"pirozhki2026"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("sign in: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do("/api/auth/password", `{"login":"babushka","password":"wrong-one"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", rec.Code)
	}
	for i := 0; i < loginFailMax; i++ {
		do("/api/auth/password", `{"login":"babushka","password":"wrong-one"}`, "")
	}
	if rec := do("/api/auth/password", `{"login":"babushka","password":"pirozhki2026"}`, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit: %d", rec.Code)
	}
	// /api/me reports the login.
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `"login":"babushka"`) {
		t.Fatalf("/api/me: %s", rec.Body.String())
	}
}

func TestAdminIssuesAccessKey(t *testing.T) {
	a, st := newTestAPI(t, false)
	u := &store.User{Username: "grandma"}
	if err := st.CreateUser(u); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/"+strconv.FormatInt(u.ID, 10)+"/access-key", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), "/cabinet/#k=") {
		t.Fatalf("admin key: %d %s", rec.Code, rec.Body.String())
	}
}
