package api

// Quick-connect join-code tests: store lifecycle, the public exchange
// (one-time, TTL, landing page does not consume), admin reseller scoping
// and the expired-subscription gate. See docs/join.md.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fenjo26/Fibr/internal/store"
)

// mintJoinCode drives POST /api/me/devices/{id}/join with the owner's
// session token and returns the code from the response.
func mintJoinCode(t *testing.T, a *API, token string, deviceID int64) string {
	t.Helper()
	rec := sessReq(a, token, http.MethodPost, "/api/me/devices/"+itoa(deviceID)+"/join", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("mint join code: status %d: %s", rec.Code, rec.Body.String())
	}
	var out joinOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad join json: %v: %s", err, rec.Body.String())
	}
	if out.Code == "" || out.URL == "" || out.ExpiresAt.IsZero() {
		t.Fatalf("incomplete join payload: %+v", out)
	}
	return out.Code
}

func joinReq(a *API, code string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/join/"+code, nil)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func TestJoinCodeStore(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)

	jc := &store.JoinCode{Code: "join1234", DeviceID: h.ownDev.ID, CreatedBy: h.reseller.ID,
		ExpiresAt: time.Now().Add(time.Minute)}
	if err := st.CreateJoinCode(jc); err != nil {
		t.Fatalf("create: %v", err)
	}
	if jc.CreatedAt.IsZero() {
		t.Fatal("CreatedAt must be filled in by CreateJoinCode")
	}

	got, err := st.ConsumeJoinCode("join1234")
	if err != nil || got.DeviceID != h.ownDev.ID || got.UsedAt == nil {
		t.Fatalf("consume: %v %+v", err, got)
	}
	if _, err := st.ConsumeJoinCode("join1234"); !errors.Is(err, store.ErrJoinCodeUsed) {
		t.Fatalf("re-consume: want ErrJoinCodeUsed, got %v", err)
	}
	if _, err := st.ConsumeJoinCode("nosuchcode"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing: want ErrNotFound, got %v", err)
	}

	expired := &store.JoinCode{Code: "expired1", DeviceID: h.ownDev.ID, CreatedBy: h.reseller.ID,
		ExpiresAt: time.Now().Add(-time.Minute)}
	if err := st.CreateJoinCode(expired); err != nil {
		t.Fatalf("create expired: %v", err)
	}
	if _, err := st.ConsumeJoinCode("expired1"); !errors.Is(err, store.ErrJoinCodeUsed) {
		t.Fatalf("expired consume: want ErrJoinCodeUsed, got %v", err)
	}

	if err := st.DeleteStaleJoinCodes(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if _, err := st.GetJoinCode("join1234"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("consumed code must be cleaned up, got %v", err)
	}
}

func TestJoinFlow(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.JoinTTL = 15 * time.Minute // harness builds Config directly; default it
	h := seedHierarchy(t, a, st)

	code := mintJoinCode(t, a, a.newSessionToken(h.ownUser.ID), h.ownDev.ID)

	// The landing page shows the code and must NOT consume it.
	rec := httptest.NewRecorder()
	landing := httptest.NewRequest(http.MethodGet, "/join/"+code, nil)
	landing.Header.Set("Accept-Language", "ru")
	a.Handler().ServeHTTP(rec, landing)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("landing: status %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), code) {
		t.Fatal("landing must display the code")
	}

	// First exchange wins and returns the rendered config.
	rec = joinReq(a, code)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange: status %d: %s", rec.Code, rec.Body.String())
	}
	var res joinResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("bad exchange json: %v", err)
	}
	if res.Protocol != store.ProtocolWG || !strings.Contains(res.Config, "[Interface]") ||
		!strings.Contains(res.Config, "PrivateKey") {
		t.Fatalf("exchange payload must carry the wg config: %+v", res)
	}
	if res.Name != h.ownDev.Name || res.DeviceID != h.ownDev.ID {
		t.Fatalf("exchange metadata mismatch: %+v", res)
	}

	// The code is one-time; the landing hit above must not have burned it.
	if rec := joinReq(a, code); rec.Code != http.StatusGone {
		t.Fatalf("re-exchange: want 410, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := joinReq(a, "nosuchcode"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown code: want 404, got %d", rec.Code)
	}
}

func TestJoinAdminScope(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.JoinTTL = 15 * time.Minute
	h := seedHierarchy(t, a, st)

	// A reseller cannot mint a code for a device outside their subtree.
	rec := sessReq(a, h.resTok, http.MethodPost, "/api/admin/devices/"+itoa(h.foreignDev.ID)+"/join", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign device: want 404, got %d: %s", rec.Code, rec.Body.String())
	}

	// The superadmin (static token) can mint for any device, and the code
	// exchanges fine.
	rec = adminReq(a, http.MethodPost, "/api/admin/devices/"+itoa(h.ownDev.ID)+"/join", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("superadmin mint: want 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var out joinOut
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad join json: %v", err)
	}
	if rec := joinReq(a, out.Code); rec.Code != http.StatusOK {
		t.Fatalf("exchange of admin code: status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestJoinExpiredAndDisabled(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.JoinTTL = 15 * time.Minute
	h := seedHierarchy(t, a, st)

	// Both codes are minted while the account is alive.
	ownTok := a.newSessionToken(h.ownUser.ID)
	code := mintJoinCode(t, a, ownTok, h.ownDev.ID)
	code2 := mintJoinCode(t, a, ownTok, h.ownDev.ID)

	// Subscription expiring after the codes were minted still gates the
	// exchange (the credential must stop flowing with the account).
	past := time.Now().Add(-time.Hour).Unix()
	if err := st.SetUserExpiry(h.ownUser.ID, &past); err != nil {
		t.Fatalf("set expiry: %v", err)
	}
	rec := joinReq(a, code)
	if rec.Code != http.StatusForbidden || errMessage(t, rec) != msgSubscriptionExpired {
		t.Fatalf("expired owner: want 403 %q, got %d: %s", msgSubscriptionExpired, rec.Code, rec.Body.String())
	}

	// With the account alive again, a disabled device answers 403.
	if err := st.SetUserExpiry(h.ownUser.ID, nil); err != nil {
		t.Fatalf("clear expiry: %v", err)
	}
	if err := st.SetDeviceEnabled(h.ownDev.ID, false); err != nil {
		t.Fatalf("disable device: %v", err)
	}
	if rec := joinReq(a, code2); rec.Code != http.StatusForbidden {
		t.Fatalf("disabled device: want 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
