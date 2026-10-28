package api

// Referral program API tests: attribution on first sign-in (including the
// invite-only gate where a referral code doubles as the invitation), the
// user-facing /api/me/referrals card, and the superadmin-only money surface
// (payments, payouts, settings). See docs/referrals.md.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fenjo26/Fibr/internal/store"
)

func uid(id int64) string { return strconv.FormatInt(id, 10) }

// refAuthPOST fires POST /api/auth/telegram with a referral code.
func refAuthPOST(a *API, initData, inviteCode, refCode string) *httptest.ResponseRecorder {
	body := `{"init_data":"` + initData + `"`
	if inviteCode != "" {
		body += `,"invite_code":"` + inviteCode + `"`
	}
	if refCode != "" {
		body += `,"referral_code":"` + refCode + `"`
	}
	body += `}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/telegram", strings.NewReader(body))
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAuthReferralAttribution(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.TelegramBotToken = testBotToken

	tid := int64(700100)
	referrer := &store.User{TelegramID: &tid, Username: "referra"}
	if err := st.CreateUser(referrer); err != nil {
		t.Fatalf("seed referrer: %v", err)
	}

	// First sign-in carrying the referrer's code: attribution recorded.
	rec := refAuthPOST(a, signedInitData(700101, "newbie"), "", referrer.ReferralCode)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth: status %d body %s", rec.Code, rec.Body.String())
	}
	newbie, err := st.GetUserByTelegramID(700101)
	if err != nil {
		t.Fatalf("newbie not created: %v", err)
	}
	if newbie.ReferredBy == nil || *newbie.ReferredBy != referrer.ID {
		t.Fatalf("attribution missing: %+v", newbie)
	}

	// Unknown codes are ignored, not fatal.
	rec = refAuthPOST(a, signedInitData(700102, "organic"), "", "nope----")
	if rec.Code != http.StatusOK {
		t.Fatalf("auth with unknown code: status %d body %s", rec.Code, rec.Body.String())
	}
	organic, _ := st.GetUserByTelegramID(700102)
	if organic.ReferredBy != nil {
		t.Fatalf("unknown code must not attribute: %+v", organic)
	}
}

func TestReferralCodeReplacesInvite(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.TelegramBotToken = testBotToken
	a.cfg.RequireInvite = true

	tid := int64(700200)
	referrer := &store.User{TelegramID: &tid, Username: "inviter"}
	if err := st.CreateUser(referrer); err != nil {
		t.Fatalf("seed referrer: %v", err)
	}

	// Invite-only mode, no invite code, valid referral: the referral IS the
	// invitation — the whole point of sharing a referral link there.
	rec := refAuthPOST(a, signedInitData(700201, "friend"), "", referrer.ReferralCode)
	if rec.Code != http.StatusOK {
		t.Fatalf("referral sign-in under require-invite: status %d body %s", rec.Code, rec.Body.String())
	}

	// No code at all: still gated.
	rec = refAuthPOST(a, signedInitData(700202, "stranger"), "", "")
	if rec.Code != http.StatusForbidden || errMessage(t, rec) != "invite required" {
		t.Fatalf("expected invite required, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestMeReferralsCard(t *testing.T) {
	a, st := newTestAPI(t, false)

	tid := int64(700300)
	referrer := &store.User{TelegramID: &tid, Username: "card-referrer"}
	if err := st.CreateUser(referrer); err != nil {
		t.Fatalf("seed: %v", err)
	}
	friend := &store.User{Username: "card-friend", ReferredBy: &referrer.ID}
	if err := st.CreateUser(friend); err != nil {
		t.Fatalf("seed friend: %v", err)
	}
	if _, err := st.RecordPayment(friend.ID, 10000, 30, "", 0); err != nil {
		t.Fatalf("payment: %v", err)
	}

	rec := sessReq(a, a.newSessionToken(referrer.ID), http.MethodGet, "/api/me/referrals", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me referrals: status %d body %s", rec.Code, rec.Body.String())
	}
	var card struct {
		Code  string `json:"code"`
		Link  string `json:"link"`
		Stats struct {
			Invited  int64 `json:"invited"`
			EarnedL1 int64 `json:"earned_l1"`
			Balance  int64 `json:"balance"`
		} `json:"stats"`
		Referrals []struct {
			Username string `json:"username"`
			Earned   int64  `json:"earned"`
		} `json:"referrals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatalf("bad json: %v: %s", err, rec.Body.String())
	}
	if card.Code != referrer.ReferralCode || card.Link == "" {
		t.Fatalf("card code/link mismatch: %+v", card)
	}
	if card.Stats.Invited != 1 || card.Stats.EarnedL1 != 1500 || card.Stats.Balance != 1500 {
		t.Fatalf("card stats mismatch: %+v", card.Stats)
	}
	if len(card.Referrals) != 1 || card.Referrals[0].Username != "card-friend" || card.Referrals[0].Earned != 1500 {
		t.Fatalf("card referrals mismatch: %+v", card.Referrals)
	}
}

func TestPaymentsAndPayoutsSuperOnly(t *testing.T) {
	a, st := newTestAPI(t, false)

	tid := int64(700400)
	reseller := &store.User{TelegramID: &tid, Username: "reseller", Role: store.RoleAdmin}
	if err := st.CreateUser(reseller); err != nil {
		t.Fatalf("seed reseller: %v", err)
	}
	referrer := &store.User{Username: "pay-referrer"}
	if err := st.CreateUser(referrer); err != nil {
		t.Fatalf("seed referrer: %v", err)
	}
	payer := &store.User{Username: "pay-payer", ReferredBy: &referrer.ID}
	if err := st.CreateUser(payer); err != nil {
		t.Fatalf("seed payer: %v", err)
	}
	resTok := a.newSessionToken(reseller.ID)

	// A reseller must not touch the money surface.
	rec := sessReq(a, resTok, http.MethodPost, "/api/admin/users/"+uid(payer.ID)+"/payments", `{"amount":10000,"days":30}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin payment must 403, got %d", rec.Code)
	}
	rec = sessReq(a, resTok, http.MethodGet, "/api/admin/payments", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin payments list must 403, got %d", rec.Code)
	}
	rec = sessReq(a, resTok, http.MethodGet, "/api/admin/referrals", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin referral overview must 403, got %d", rec.Code)
	}
	rec = sessReq(a, resTok, http.MethodGet, "/api/admin/settings", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin settings must 403, got %d", rec.Code)
	}

	// The superadmin records a top-up: deadline + commissions in one shot.
	rec = adminReq(a, http.MethodPost, "/api/admin/users/"+uid(payer.ID)+"/payments", `{"amount":20000,"days":30,"note":"crypto"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("superadmin payment: status %d body %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Earnings []struct {
			BeneficiaryID int64 `json:"beneficiary_id"`
			Amount        int64 `json:"amount"`
		} `json:"earnings"`
		NewExpiresAt *string `json:"new_expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(res.Earnings) != 1 || res.Earnings[0].BeneficiaryID != referrer.ID || res.Earnings[0].Amount != 3000 {
		t.Fatalf("payment earnings mismatch: %+v", res.Earnings)
	}
	if res.NewExpiresAt == nil {
		t.Fatal("deadline must move for a 30-day payment")
	}

	// Ledger lists work for the superadmin.
	rec = adminReq(a, http.MethodGet, "/api/admin/payments", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("payments list: %d", rec.Code)
	}
	rec = adminReq(a, http.MethodGet, "/api/admin/referrals", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("referral overview: %d", rec.Code)
	}

	// Payout: overdraft refused (409), the exact balance goes through.
	rec = adminReq(a, http.MethodPost, "/api/admin/referrals/payouts", `{"user_id":`+uid(referrer.ID)+`,"amount":999999}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("overdraft payout must 409, got %d", rec.Code)
	}
	rec = adminReq(a, http.MethodPost, "/api/admin/referrals/payouts", `{"user_id":`+uid(referrer.ID)+`,"amount":3000,"note":"usdt"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("payout: status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestSettingsPatchRoundTrip(t *testing.T) {
	a, _ := newTestAPI(t, false)

	rec := adminReq(a, http.MethodPatch, "/api/admin/settings",
		`{"referral_enabled":true,"referral_percent_l1":20,"referral_percent_l2":7,"currency":"₽","contact_info":"@fibr_admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch settings: status %d body %s", rec.Code, rec.Body.String())
	}
	var rs store.ReferralSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &rs); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if !rs.Enabled || rs.PercentL1 != 20 || rs.PercentL2 != 7 || rs.Currency != "₽" || rs.ContactInfo != "@fibr_admin" {
		t.Fatalf("settings round-trip mismatch: %+v", rs)
	}

	// Out-of-range percentages are rejected.
	rec = adminReq(a, http.MethodPatch, "/api/admin/settings", `{"referral_percent_l1":150}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("percent clamp expected 400, got %d", rec.Code)
	}

	// The contact line is public (the expired screen needs it pre-auth).
	rec = adminReq(a, http.MethodGet, "/api/public/info", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("public info: %d", rec.Code)
	}
	var info struct {
		ContactInfo string `json:"contact_info"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || info.ContactInfo != "@fibr_admin" {
		t.Fatalf("public contact mismatch: %v %s", err, rec.Body.String())
	}
}
