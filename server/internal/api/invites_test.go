package api

// Invite / self-enroll tests: store lifecycle, the registration gate behind
// FIBR_REQUIRE_INVITE, and the admin invite endpoints.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fenjo26/Fibr/internal/store"
)

const testBotToken = "7000000001:INVITE-TEST-TOKEN"

func signedInitData(tgUserID int64, username string) string {
	params := map[string]string{
		"auth_date": strconv.FormatInt(time.Now().Unix(), 10),
		"query_id":  "AAINVITE0000000000000000",
		"user":      `{"id":` + strconv.FormatInt(tgUserID, 10) + `,"first_name":"Test","username":"` + username + `","language_code":"ru"}`,
	}
	return signInitData(testBotToken, params)
}

// authPOST fires POST /api/auth/telegram and returns the recorder.
func authPOST(a *API, initData, inviteCode string) *httptest.ResponseRecorder {
	body := `{"init_data":"` + initData + `"`
	if inviteCode != "" {
		body += `,"invite_code":"` + inviteCode + `"`
	}
	body += `}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/telegram", strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func adminReq(a *API, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.AdminToken)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func errMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad error json: %v: %s", err, rec.Body.String())
	}
	return out.Error
}

func TestInviteStoreLifecycle(t *testing.T) {
	_, st := newTestAPI(t, false)

	inv := &store.Invite{Code: "abc123DEF9", Note: "for Petro", CreatedBy: 7}
	if err := st.CreateInvite(inv); err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if inv.CreatedAt.IsZero() {
		t.Fatal("CreatedAt must be filled in by CreateInvite")
	}

	got, err := st.GetInvite("abc123DEF9")
	if err != nil {
		t.Fatalf("get invite: %v", err)
	}
	if got.Note != "for Petro" || got.CreatedBy != 7 || got.UsedBy != nil || got.UsedAt != nil {
		t.Fatalf("invite round-trip mismatch: %+v", got)
	}
	if _, err := st.GetInvite("nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	list, err := st.ListInvites()
	if err != nil || len(list) != 1 || list[0].Code != "abc123DEF9" {
		t.Fatalf("list invites: %v %+v", err, list)
	}

	// Consume once, then it is spent. used_by is a real FK — create the user.
	owner := &store.User{Username: "owner"}
	if err := st.CreateUser(owner); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	uid := owner.ID
	if err := st.MarkUsed("abc123DEF9", uid); err != nil {
		t.Fatalf("mark used: %v", err)
	}
	if err := st.MarkUsed("abc123DEF9", owner.ID+1000); !errors.Is(err, store.ErrInviteUsed) {
		t.Fatalf("second use must be ErrInviteUsed, got %v", err)
	}
	if err := st.MarkUsed("missing", uid); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown code must be ErrNotFound, got %v", err)
	}

	got, err = st.GetInvite("abc123DEF9")
	if err != nil {
		t.Fatalf("reload invite: %v", err)
	}
	if got.UsedBy == nil || *got.UsedBy != uid || got.UsedAt == nil || got.UsedAt.Before(got.CreatedAt) {
		t.Fatalf("used fields mismatch: %+v", got)
	}

	if err := st.DeleteInvite("abc123DEF9"); err != nil {
		t.Fatalf("delete invite: %v", err)
	}
	if err := st.DeleteInvite("abc123DEF9"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("double delete must be ErrNotFound, got %v", err)
	}
	if _, err := st.GetInvite("abc123DEF9"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestGenerateInviteCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code, err := GenerateInviteCode()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if len(code) != inviteCodeLen {
			t.Fatalf("code length %d: %s", len(code), code)
		}
		for _, c := range code {
			if !strings.ContainsRune(inviteAlphabet, c) {
				t.Fatalf("code %q contains char %q outside alphabet", code, c)
			}
		}
		if seen[code] {
			t.Fatalf("duplicate code generated: %s", code)
		}
		seen[code] = true
	}
}

func TestInviteLink(t *testing.T) {
	cases := map[string]string{
		"vpn.example.com":         "https://vpn.example.com/miniapp/?invite=CODE",
		"vpn.example.com/":        "https://vpn.example.com/miniapp/?invite=CODE",
		"https://vpn.example.com": "https://vpn.example.com/miniapp/?invite=CODE",
		"":                        "/miniapp/?invite=CODE",
		"http://localhost":        "/miniapp/?invite=CODE",
		"localhost":               "/miniapp/?invite=CODE",
		"localhost:8080":          "/miniapp/?invite=CODE",
		"127.0.0.1":               "/miniapp/?invite=CODE",
	}
	for domain, want := range cases {
		if got := InviteLink(domain, "CODE"); got != want {
			t.Errorf("InviteLink(%q) = %q, want %q", domain, got, want)
		}
	}
}

// TestInviteGateRegistration walks the whole self-enroll flow with
// FIBR_REQUIRE_INVITE=true: no code -> 403, bad code -> 403, good code ->
// registered, repeat sign-in without code -> ok, code is one-time.
func TestInviteGateRegistration(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.TelegramBotToken = testBotToken
	a.cfg.RequireInvite = true

	// New user, no invite code: rejected, no user row.
	rec := authPOST(a, signedInitData(501, "nocode"), "")
	if rec.Code != http.StatusForbidden || errMessage(t, rec) != "invite required" {
		t.Fatalf("no code: status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetUserByTelegramID(501); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("user must not be created without a code")
	}

	// New user, garbage code: rejected with a different message.
	rec = authPOST(a, signedInitData(502, "badcode"), "DOESNOTEX1ST")
	if rec.Code != http.StatusForbidden || errMessage(t, rec) != "invalid invite" {
		t.Fatalf("bad code: status %d body %s", rec.Code, rec.Body.String())
	}

	// Admin creates an invite.
	rec = adminReq(a, http.MethodPost, "/api/admin/invites", `{"note":"for Olya"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create invite: status %d body %s", rec.Code, rec.Body.String())
	}
	var created inviteOut
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("bad invite json: %v", err)
	}
	if created.Note != "for Olya" {
		t.Fatalf("note mismatch: %+v", created)
	}
	if want := "https://vpn.example.com/cabinet/?invite=" + created.Code; created.Link != want {
		t.Fatalf("link %q, want %q", created.Link, want)
	}

	// New user with the code: registered, gets a session token.
	rec = authPOST(a, signedInitData(503, "olya"), created.Code)
	if rec.Code != http.StatusOK {
		t.Fatalf("register with code: status %d body %s", rec.Code, rec.Body.String())
	}
	var sess struct {
		Token string      `json:"token"`
		User  *store.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil || sess.Token == "" || sess.User == nil {
		t.Fatalf("bad session json: %v: %s", err, rec.Body.String())
	}
	olya, err := st.GetUserByTelegramID(503)
	if err != nil || olya.Username != "olya" {
		t.Fatalf("olya must exist: %v %+v", err, olya)
	}

	// The invite is now consumed: used_by recorded with the username joined
	// into the admin list.
	listRec := adminReq(a, http.MethodGet, "/api/admin/invites", "")
	if listRec.Code != http.StatusOK {
		t.Fatalf("list invites: status %d", listRec.Code)
	}
	var invites []inviteOut
	if err := json.Unmarshal(listRec.Body.Bytes(), &invites); err != nil || len(invites) != 1 {
		t.Fatalf("list invites: %v %s", err, listRec.Body.String())
	}
	if invites[0].UsedBy == nil || *invites[0].UsedBy != olya.ID || invites[0].Username != "olya" || invites[0].UsedAt == nil {
		t.Fatalf("invite not marked used: %+v", invites[0])
	}

	// Existing user signs in again WITHOUT any code: fine.
	rec = authPOST(a, signedInitData(503, "olya"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat sign-in: status %d body %s", rec.Code, rec.Body.String())
	}

	// The code is one-time: a second new user is rejected and not created.
	rec = authPOST(a, signedInitData(504, "freeloader"), created.Code)
	if rec.Code != http.StatusForbidden || errMessage(t, rec) != "invalid invite" {
		t.Fatalf("code reuse: status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetUserByTelegramID(504); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("code reuse must not create a user")
	}
}

// TestFreeRegistrationWithoutRequireInvite: with the flag off (default) the
// old behaviour holds — anyone signing in via the Mini App is registered.
func TestFreeRegistrationWithoutRequireInvite(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.TelegramBotToken = testBotToken
	if a.cfg.RequireInvite {
		t.Fatal("default must be free registration")
	}

	rec := authPOST(a, signedInitData(601, "free"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("free registration: status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetUserByTelegramID(601); err != nil {
		t.Fatalf("user must be auto-registered: %v", err)
	}

	// A stray invite_code in the body is simply ignored.
	rec = authPOST(a, signedInitData(602, "stray"), "whatever")
	if rec.Code != http.StatusOK {
		t.Fatalf("stray code must not break free registration: %d", rec.Code)
	}
}

// TestInviteAdminCRUD covers the admin endpoints' happy path and auth.
func TestInviteAdminCRUD(t *testing.T) {
	a, _ := newTestAPI(t, false)

	// Create (empty body is fine — note is optional).
	rec := adminReq(a, http.MethodPost, "/api/admin/invites", `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status %d body %s", rec.Code, rec.Body.String())
	}
	var created inviteOut
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(created.Code) != inviteCodeLen {
		t.Fatalf("code length: %q", created.Code)
	}

	// List contains it.
	var invites []inviteOut
	listRec := adminReq(a, http.MethodGet, "/api/admin/invites", "")
	if err := json.Unmarshal(listRec.Body.Bytes(), &invites); err != nil || len(invites) != 1 {
		t.Fatalf("list: %v %s", err, listRec.Body.String())
	}

	// Delete -> 204, then delete again -> 404.
	if rec := adminReq(a, http.MethodDelete, "/api/admin/invites/"+created.Code, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d", rec.Code)
	}
	if rec := adminReq(a, http.MethodDelete, "/api/admin/invites/"+created.Code, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("double delete: status %d", rec.Code)
	}

	// Admin endpoints require the admin token (a user session must not pass).
	u := &store.User{Username: "mallory"}
	if err := a.store.CreateUser(u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/invites", nil)
	req.Header.Set("Authorization", "Bearer "+a.newSessionToken(u.ID))
	rr := httptest.NewRecorder()
	a.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-admin list must be 403, got %d", rr.Code)
	}
}

func TestInviteShareLink(t *testing.T) {
	if got, want := InviteShareLink("vpn.example.com", "fibr_bot", "CODE"), "https://vpn.example.com/cabinet/?invite=CODE"; got != want {
		t.Errorf("with bot: %q, want %q", got, want)
	}
	if got, want := InviteShareLink("", "", "CODE"), "/cabinet/?invite=CODE"; got != want {
		t.Errorf("dev: %q, want %q", got, want)
	}
	if got, want := ReferralShareLink("vpn.example.com", "fibr_bot", "R1"), "https://t.me/fibr_bot?start=ref_R1"; got != want {
		t.Errorf("referral: %q, want %q", got, want)
	}
}
