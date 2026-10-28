package api

// Referral program endpoints (see docs/referrals.md): the Mini App's own
// referral card, the superadmin's payment ledger (top-ups that extend the
// subscription and mint commissions), the referral overview and manual
// payouts, and the superadmin-editable program settings. Money is recorded
// in minor units (kopecks) as INTEGERs; the actual cash always moves outside
// the system (docs/billing.md).

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fenjo26/Fibr/internal/store"
)

// ReferralLink renders the shareable enrollment link for a referral code —
// the same dev-mode rules as InviteLink: without a public domain it stays a
// relative path (still copyable in the panel).
func ReferralLink(domain, code string) string {
	d := strings.TrimSpace(domain)
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	d = strings.TrimSuffix(strings.TrimSpace(d), "/")
	if d == "" || d == "localhost" || strings.HasPrefix(d, "localhost:") ||
		d == "127.0.0.1" || strings.HasPrefix(d, "127.0.0.1:") {
		return "/miniapp/?ref=" + code
	}
	return "https://" + d + "/miniapp/?ref=" + code
}

// handleMeReferrals serves the caller's referral card: code, share link,
// program terms, aggregates and the invited accounts.
func (a *API) handleMeReferrals(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	u, err := a.store.GetUser(id.userID)
	if err != nil {
		a.errJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	rs, err := a.store.ReferralSettings()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	stats, err := a.store.ReferralStatsOf(u.ID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	referrals, err := a.store.ListReferralsOf(u.ID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":         u.ReferralCode,
		"link":         ReferralLink(a.cfg.Domain, u.ReferralCode),
		"enabled":      rs.Enabled,
		"currency":     rs.Currency,
		"percent_l1":   rs.PercentL1,
		"percent_l2":   rs.PercentL2,
		"contact_info": rs.ContactInfo,
		"stats":        stats,
		"referrals":    referrals,
	})
}

// handleAdminCreatePayment records a top-up for one user: the amount goes to
// the ledger, `days` extend the subscription (same arithmetic as the +30
// button, now server-side) and referral commissions are minted along the
// attribution chain. Selling access is the installation owner's alone, like
// every other expires_at write.
func (a *API) handleAdminCreatePayment(w http.ResponseWriter, r *http.Request) {
	caller := ident(r)
	if !caller.isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return
	}
	uid, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, err := a.store.GetUser(uid); err != nil {
		a.errJSON(w, http.StatusNotFound, "user not found")
		return
	}
	var body struct {
		Amount int64  `json:"amount"` // minor units, > 0
		Days   int64  `json:"days"`   // optional subscription days (>= 0)
		Note   string `json:"note"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.store.RecordPayment(uid, body.Amount, body.Days, strings.TrimSpace(body.Note), caller.userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.errJSON(w, http.StatusNotFound, "user not found")
			return
		}
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if res.NewExpiresAt != nil {
		// The deadline moved: bring WG peers / xray clients in line now.
		a.syncPeers()
		a.syncXray()
	}
	a.log.Info("payment recorded", "user_id", uid, "amount", body.Amount, "days", body.Days, "earnings", len(res.Earnings), "by", caller.userID)
	writeJSON(w, http.StatusCreated, res)
}

// handleAdminListPayments serves the recent payments ledger (superadmin
// only — it discloses the whole installation's revenue).
func (a *API) handleAdminListPayments(w http.ResponseWriter, r *http.Request) {
	if !ident(r).isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return
	}
	payments, err := a.store.ListPayments(100)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, payments)
}

// handleAdminReferralOverview serves the superadmin's "who invited whom"
// table: every account the program touches, with attribution and balances.
func (a *API) handleAdminReferralOverview(w http.ResponseWriter, r *http.Request) {
	if !ident(r).isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return
	}
	rows, err := a.store.ReferralOverview()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	rs, err := a.store.ReferralSettings()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	payouts, err := a.store.ListPayouts(50)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rows":     rows,
		"settings": rs,
		"payouts":  payouts,
	})
}

// handleAdminCreatePayout records money handed to a user outside the system
// and subtracts it from their referral balance.
func (a *API) handleAdminCreatePayout(w http.ResponseWriter, r *http.Request) {
	caller := ident(r)
	if !caller.isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return
	}
	var body struct {
		UserID int64  `json:"user_id"`
		Amount int64  `json:"amount"` // minor units, > 0
		Note   string `json:"note"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := a.store.CreatePayout(body.UserID, body.Amount, strings.TrimSpace(body.Note), caller.userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInsufficientBalance) {
			a.errJSON(w, http.StatusConflict, err.Error())
			return
		}
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	a.log.Info("referral payout recorded", "user_id", body.UserID, "amount", body.Amount, "by", caller.userID)
	writeJSON(w, http.StatusCreated, p)
}

/* ------------------------- program settings ------------------------- */

// handleAdminGetSettings serves the referral program configuration with
// defaults resolved (superadmin only).
func (a *API) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) {
	if !ident(r).isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return
	}
	rs, err := a.store.ReferralSettings()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

// handleAdminPatchSettings updates the referral program configuration. Only
// the passed fields change; percent values are clamped to 0..100 and text
// fields to sane lengths.
func (a *API) handleAdminPatchSettings(w http.ResponseWriter, r *http.Request) {
	if !ident(r).isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return
	}
	var body struct {
		ReferralEnabled   *bool   `json:"referral_enabled"`
		ReferralPercentL1 *int64  `json:"referral_percent_l1"`
		ReferralPercentL2 *int64  `json:"referral_percent_l2"`
		Currency          *string `json:"currency"`
		ContactInfo       *string `json:"contact_info"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	set := func(key string, v string) bool {
		if err := a.store.SetSetting(key, v); err != nil {
			a.errJSON(w, http.StatusInternalServerError, "storage error")
			return false
		}
		return true
	}
	if body.ReferralEnabled != nil {
		v := "0"
		if *body.ReferralEnabled {
			v = "1"
		}
		if !set(store.SettingReferralEnabled, v) {
			return
		}
	}
	if body.ReferralPercentL1 != nil {
		if *body.ReferralPercentL1 < 0 || *body.ReferralPercentL1 > 100 {
			a.errJSON(w, http.StatusBadRequest, "referral_percent_l1 must be 0..100")
			return
		}
		if !set(store.SettingReferralPercentL1, strconv.FormatInt(*body.ReferralPercentL1, 10)) {
			return
		}
	}
	if body.ReferralPercentL2 != nil {
		if *body.ReferralPercentL2 < 0 || *body.ReferralPercentL2 > 100 {
			a.errJSON(w, http.StatusBadRequest, "referral_percent_l2 must be 0..100")
			return
		}
		if !set(store.SettingReferralPercentL2, strconv.FormatInt(*body.ReferralPercentL2, 10)) {
			return
		}
	}
	if body.Currency != nil {
		c := strings.TrimSpace(*body.Currency)
		if len(c) > 16 {
			a.errJSON(w, http.StatusBadRequest, "currency too long")
			return
		}
		if !set(store.SettingCurrency, c) {
			return
		}
	}
	if body.ContactInfo != nil {
		c := strings.TrimSpace(*body.ContactInfo)
		if len(c) > 2000 {
			a.errJSON(w, http.StatusBadRequest, "contact_info too long")
			return
		}
		if !set(store.SettingContactInfo, c) {
			return
		}
	}
	rs, err := a.store.ReferralSettings()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, rs)
}
