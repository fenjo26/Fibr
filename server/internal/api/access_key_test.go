package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fenjo26/Fibr/internal/store"
)

// TestAccessKeyLifecycle walks the server-login surface end to end: key
// generation over the authenticated endpoint, key-based sign-in, key-based
// API access (identify resolves the owner's identity from the hash),
// rotation killing the old key, and revocation killing key access entirely.
func TestAccessKeyLifecycle(t *testing.T) {
	a, st := newTestAPI(t, false)

	u := &store.User{Username: "carol"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("user: %v", err)
	}
	session := a.newSessionToken(u.ID)
	bearerSession := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+session) }

	call := func(method, path, body string, auth func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		var rd strings.Reader
		if body != "" {
			rd = *strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, &rd)
		if auth != nil {
			auth(req)
		}
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}

	// No key yet: /api/me reports has_access_key=false.
	rec := call(http.MethodGet, "/api/me", "", bearerSession)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: status %d body %s", rec.Code, rec.Body.String())
	}
	var me struct {
		HasAccessKey bool `json:"has_access_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("me json: %v", err)
	}
	if me.HasAccessKey {
		t.Fatal("fresh account must not report has_access_key=true")
	}

	// Unauthenticated key management is refused (auth middleware: userID 0).
	if rec := call(http.MethodPost, "/api/me/access-key", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated key create must 401, got %d", rec.Code)
	}

	// Generate: 32 chars, returned as plaintext exactly once.
	rec = call(http.MethodPost, "/api/me/access-key", "", bearerSession)
	if rec.Code != http.StatusCreated {
		t.Fatalf("key create: status %d body %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("key json: %v", err)
	}
	if len(created.Key) != accessKeyLen {
		t.Fatalf("key length: %d want %d (%q)", len(created.Key), accessKeyLen, created.Key)
	}
	for _, c := range created.Key {
		if !strings.ContainsRune(inviteAlphabet, c) {
			t.Fatalf("key char %q outside the alphabet", c)
		}
	}

	// The key never appears in any account-facing payload again: /api/me
	// exposes only the boolean "a key exists".
	rec = call(http.MethodGet, "/api/me", "", bearerSession)
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if !me.HasAccessKey {
		t.Fatal("has_access_key must flip to true after generation")
	}
	if strings.Contains(rec.Body.String(), created.Key) {
		t.Fatal("plaintext key leaked through /api/me")
	}

	// A repeat call rotates: a NEW key is returned (the old plaintext is
	// unrecoverable by design).
	rec = call(http.MethodPost, "/api/me/access-key", "", bearerSession)
	if rec.Code != http.StatusCreated {
		t.Fatalf("key rotate: status %d", rec.Code)
	}
	var rotated struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("rotate json: %v", err)
	}
	if rotated.Key == created.Key || len(rotated.Key) != accessKeyLen {
		t.Fatalf("rotation must mint a fresh key, got %q", rotated.Key)
	}

	// Sign-in with the rotated key: same shape as the telegram login.
	rec = call(http.MethodPost, "/api/auth/key", `{"key":"`+rotated.Key+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth by key: status %d body %s", rec.Code, rec.Body.String())
	}
	var auth struct {
		Token string     `json:"token"`
		User  store.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &auth); err != nil {
		t.Fatalf("auth json: %v", err)
	}
	if auth.Token == "" || auth.User.ID != u.ID {
		t.Fatalf("auth by key payload: %+v", auth)
	}
	// The issued session token works like any other.
	rec = call(http.MethodGet, "/api/me", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+auth.Token)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("me with key-issued session: status %d", rec.Code)
	}

	// The access key itself works as a Bearer for the user surface
	// (identify resolves it by hash; the key carries the owner's rights).
	rec = call(http.MethodGet, "/api/me", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+rotated.Key)
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("me with access key: status %d body %s", rec.Code, rec.Body.String())
	}

	// The pre-rotation key is dead everywhere: sign-in and Bearer use.
	if rec = call(http.MethodPost, "/api/auth/key", `{"key":"`+created.Key+`"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old key sign-in must 401, got %d", rec.Code)
	}
	if rec = call(http.MethodGet, "/api/me", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+created.Key)
	}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old key bearer must 401, got %d", rec.Code)
	}

	// Broken/garbage keys never authenticate.
	if rec = call(http.MethodPost, "/api/auth/key", `{"key":"no-such-key-0000000000000000"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad key sign-in must 401, got %d", rec.Code)
	}
	if rec = call(http.MethodPost, "/api/auth/key", `{"key":""}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty key must 400, got %d", rec.Code)
	}

	// Revoke with the key itself (it still owns the account), then both the
	// key and its issued session die for NEW requests... the session stays
	// valid (30-day token), but the key stops identifying the caller.
	rec = call(http.MethodDelete, "/api/me/access-key", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+rotated.Key)
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("key revoke: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec = call(http.MethodGet, "/api/me", "", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+rotated.Key)
	}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key bearer must 401, got %d", rec.Code)
	}
	if rec = call(http.MethodPost, "/api/auth/key", `{"key":"`+rotated.Key+`"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key sign-in must 401, got %d", rec.Code)
	}
	rec = call(http.MethodGet, "/api/me", "", bearerSession)
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.HasAccessKey {
		t.Fatal("has_access_key must flip back to false after revoke")
	}
}

// TestAccessKeyDisabledExpired: a disabled or expired account cannot sign in
// with its key, and a key stops identifying mid-session once the deadline
// passes (same 403 the session tokens get).
func TestAccessKeyDisabledExpired(t *testing.T) {
	a, st := newTestAPI(t, false)

	u := &store.User{Username: "dave"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("user: %v", err)
	}
	rec := httptest.NewRequest(http.MethodPost, "/api/me/access-key", nil)
	rec.Header.Set("Authorization", "Bearer "+a.newSessionToken(u.ID))
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, rec)
	if w.Code != http.StatusCreated {
		t.Fatalf("key create: %d", w.Code)
	}
	var created struct {
		Key string `json:"key"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// Disabled: sign-in refused with 403.
	if err := st.SetUserDisabled(u.ID, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/key", strings.NewReader(`{"key":"`+created.Key+`"}`))
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled key sign-in must 403, got %d", w.Code)
	}
	// The key no longer identifies the caller either.
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+created.Key)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled key bearer must 401, got %d", w.Code)
	}

	// Re-enable, then expire: the subscription-expired path (403 with the
	// localized reason) for sign-in and Bearer use alike.
	if err := st.SetUserDisabled(u.ID, false); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	past := int64(1) // 1970-01-01: always expired
	if err := st.SetUserExpiry(u.ID, &past); err != nil {
		t.Fatalf("expire: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/auth/key", strings.NewReader(`{"key":"`+created.Key+`"}`))
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), msgSubscriptionExpired) {
		t.Fatalf("expired key sign-in must 403 %q, got %d %s", msgSubscriptionExpired, w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+created.Key)
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), msgSubscriptionExpired) {
		t.Fatalf("expired key bearer must 403 %q, got %d %s", msgSubscriptionExpired, w.Code, w.Body.String())
	}
}
