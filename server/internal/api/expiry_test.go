package api

// Subscription-expiry tests (manual monetization, see docs/billing.md):
// who may set deadlines, what an expired account can no longer do, and the
// owner cascade — an expired admin takes their whole branch down.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/fenjo26/Fibr/internal/store"
)

// TestPatchUserExpiry: PATCH /api/admin/users/{id} expires_at is the
// superadmin's alone (an admin session gets 403 even for their own users);
// null clears the deadline, absent leaves it alone, junk is a 400.
func TestPatchUserExpiry(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)
	own := "/api/admin/users/" + itoa(h.ownUser.ID)

	// A reseller must not sell (or grant) time to their own users.
	rec := sessReq(a, h.resTok, http.MethodPatch, own, `{"expires_at":"2030-01-01T00:00:00Z"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin patching expires_at: status %d body %s", rec.Code, rec.Body.String())
	}

	// The superadmin sets a deadline; the row and the JSON echo it back.
	future := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	rec = adminReq(a, http.MethodPatch, own, `{"expires_at":"`+future+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("superadmin patching expires_at: status %d body %s", rec.Code, rec.Body.String())
	}
	var patched store.User
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil || patched.ExpiresAt == nil {
		t.Fatalf("patched expires_at missing from response: %v %s", err, rec.Body.String())
	}
	got, _ := st.GetUser(h.ownUser.ID)
	if got.ExpiresAt == nil {
		t.Fatal("expires_at not persisted")
	}

	// An unrelated PATCH (disabled) must not clobber the deadline.
	rec = adminReq(a, http.MethodPatch, own, `{"disabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch disabled: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ = st.GetUser(h.ownUser.ID)
	if got.ExpiresAt == nil {
		t.Fatal("absent expires_at must leave the deadline alone")
	}

	// Explicit null = unlimited.
	rec = adminReq(a, http.MethodPatch, own, `{"expires_at":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clearing expires_at: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ = st.GetUser(h.ownUser.ID)
	if got.ExpiresAt != nil {
		t.Fatalf("null must clear the deadline, got %+v", got.ExpiresAt)
	}

	// Junk values stay 400.
	for _, body := range []string{`{"expires_at":"tomorrow"}`, `{"expires_at":12345}`, `{"expires_at":"31.12.2026"}`} {
		if rec := adminReq(a, http.MethodPatch, own, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("bad expires_at %s: status %d body %s", body, rec.Code, rec.Body.String())
		}
	}
}

// TestExpiredUserGates: an account past its own deadline cannot sign in
// (403 "subscription expired"), an already-issued session is refused with the
// same reason, and while paid /api/me exposes expires_at.
func TestExpiredUserGates(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.TelegramBotToken = testBotToken

	tid := int64(777)
	u := &store.User{TelegramID: &tid, Username: "subscriber"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// While paid: sign-in works, /api/me carries expires_at.
	paid := time.Now().Add(time.Hour).Unix()
	if err := st.SetUserExpiry(u.ID, &paid); err != nil {
		t.Fatalf("set expiry: %v", err)
	}
	rec := authPOST(a, signedInitData(777, "subscriber"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("paid sign-in: status %d body %s", rec.Code, rec.Body.String())
	}
	var sess struct {
		Token string      `json:"token"`
		User  *store.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil || sess.Token == "" {
		t.Fatalf("bad session json: %v %s", err, rec.Body.String())
	}
	if sess.User == nil || sess.User.ExpiresAt == nil {
		t.Fatalf("expires_at missing from auth user: %+v", sess.User)
	}

	rec = sessReq(a, sess.Token, http.MethodGet, "/api/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("paid /api/me: status %d body %s", rec.Code, rec.Body.String())
	}
	var me struct {
		User *store.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || me.User == nil || me.User.ExpiresAt == nil {
		t.Fatalf("/api/me must expose expires_at: %v %s", err, rec.Body.String())
	}

	// Past the deadline both gates slam shut with the machine-readable reason.
	past := time.Now().Add(-time.Minute).Unix()
	if err := st.SetUserExpiry(u.ID, &past); err != nil {
		t.Fatalf("expire: %v", err)
	}
	rec = authPOST(a, signedInitData(777, "subscriber"), "")
	if rec.Code != http.StatusForbidden || errMessage(t, rec) != "subscription expired" {
		t.Fatalf("expired sign-in: status %d body %s", rec.Code, rec.Body.String())
	}
	rec = sessReq(a, sess.Token, http.MethodGet, "/api/me", "")
	if rec.Code != http.StatusForbidden || errMessage(t, rec) != "subscription expired" {
		t.Fatalf("expired session on /api/me: status %d body %s", rec.Code, rec.Body.String())
	}
}

// TestUserUnderExpiredAdmin: the owner cascade — an expired admin's users are
// effectively expired even with a NULL deadline of their own; the users list
// reports the computed flag and the overview counts both.
func TestUserUnderExpiredAdmin(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st) // reseller owns ownUser (deadline NULL)

	past := time.Now().Add(-time.Hour).Unix()
	if err := st.SetUserExpiry(h.reseller.ID, &past); err != nil {
		t.Fatalf("expire admin: %v", err)
	}

	// The branch user inherits the admin's deadline: their session is out.
	tok := a.newSessionToken(h.ownUser.ID)
	rec := sessReq(a, tok, http.MethodGet, "/api/me", "")
	if rec.Code != http.StatusForbidden || errMessage(t, rec) != "subscription expired" {
		t.Fatalf("branch user /api/me: status %d body %s", rec.Code, rec.Body.String())
	}

	// The list reports the effective verdict for both; strangers stay clean.
	rec = adminReq(a, http.MethodGet, "/api/admin/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list users: status %d", rec.Code)
	}
	var users []struct {
		ID      int64      `json:"id"`
		Expired bool       `json:"expired"`
		Expires *time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil {
		t.Fatalf("bad json: %v %s", err, rec.Body.String())
	}
	verdict := map[int64]bool{}
	for _, u := range users {
		verdict[u.ID] = u.Expired
	}
	if !verdict[h.reseller.ID] || !verdict[h.ownUser.ID] {
		t.Fatalf("admin and their user must read as expired, got %v", verdict)
	}
	if verdict[h.foreign.ID] || verdict[h.loner.ID] {
		t.Fatalf("unrelated accounts must not read as expired, got %v", verdict)
	}

	// The overview counts the expired pair (superadmin-only endpoint).
	rec = adminReq(a, http.MethodGet, "/api/admin/overview", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("overview: status %d", rec.Code)
	}
	var ov struct {
		Totals struct {
			Expired int `json:"expired"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ov); err != nil {
		t.Fatalf("bad overview json: %v %s", err, rec.Body.String())
	}
	if ov.Totals.Expired != 2 {
		t.Fatalf("overview totals.expired: got %d want 2", ov.Totals.Expired)
	}
}
