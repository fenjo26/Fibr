package store

// Referral program tests: code minting/backfill, two-level commission
// minting inside RecordPayment, the subscription extension arithmetic,
// balance math and the payout guard (see docs/referrals.md).

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReferralCodeMintedAndBackfilled(t *testing.T) {
	s := openTestStore(t)

	// New accounts get a code at creation…
	tid := int64(100600)
	u := &User{TelegramID: &tid, Username: "alice"}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(u.ReferralCode) != 8 {
		t.Fatalf("expected an 8-char referral code, got %q", u.ReferralCode)
	}
	got, err := s.GetUserByReferralCode(u.ReferralCode)
	if err != nil || got.ID != u.ID {
		t.Fatalf("resolve by code: %+v err %v", got, err)
	}
	if _, err := s.GetUserByReferralCode("nope----"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// …and a pre-program database gets codes on Open (backfill): drop a
	// NULL-code row straight in, reopen, expect a code.
	if _, err := s.db.Exec(`UPDATE users SET referral_code = NULL WHERE id = ?`, u.ID); err != nil {
		t.Fatalf("null out code: %v", err)
	}
	path := filepath.Join(t.TempDir(), "backfill.db")
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	// Open() with a fresh path has no rows; simulate legacy by re-running the
	// backfill against the first store (idempotent by WHERE NULL).
	if err := s.backfillReferralCodes(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	after, err := s.GetUser(u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(after.ReferralCode) != 8 {
		t.Fatalf("backfill did not mint a code: %q", after.ReferralCode)
	}
}

func mustRefUser(t *testing.T, s *Store, name string, referredBy *int64) *User {
	t.Helper()
	u := &User{Username: name, ReferredBy: referredBy}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return u
}

func TestRecordPaymentTwoLevelCommissions(t *testing.T) {
	s := openTestStore(t)

	grand := mustRefUser(t, s, "grand", nil)
	ref := mustRefUser(t, s, "ref", &grand.ID)
	payer := mustRefUser(t, s, "payer", &ref.ID)

	// 500.00 paid, 30 days: L1 = 15% = 75.00, L2 = 5% = 25.00 (defaults).
	res, err := s.RecordPayment(payer.ID, 50000, 30, "first top-up", 0)
	if err != nil {
		t.Fatalf("record payment: %v", err)
	}
	if len(res.Earnings) != 2 {
		t.Fatalf("expected 2 earnings, got %d: %+v", len(res.Earnings), res.Earnings)
	}
	e1, e2 := res.Earnings[0], res.Earnings[1]
	if e1.BeneficiaryID != ref.ID || e1.Level != 1 || e1.Amount != 7500 {
		t.Fatalf("L1 earning mismatch: %+v", e1)
	}
	if e2.BeneficiaryID != grand.ID || e2.Level != 2 || e2.Amount != 2500 {
		t.Fatalf("L2 earning mismatch: %+v", e2)
	}

	// The deadline moved 30 days forward from now.
	p, err := s.GetUser(payer.ID)
	if err != nil || p.ExpiresAt == nil {
		t.Fatalf("payer deadline after payment: %+v err %v", p, err)
	}
	want := time.Now().Add(29 * 24 * time.Hour)
	if p.ExpiresAt.Before(want) {
		t.Fatalf("deadline not extended: %v", p.ExpiresAt)
	}

	// Aggregates.
	st, err := s.ReferralStatsOf(ref.ID)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Invited != 1 || st.EarnedL1 != 7500 || st.EarnedL2 != 0 || st.Balance != 7500 {
		t.Fatalf("ref stats mismatch: %+v", st)
	}
	stg, _ := s.ReferralStatsOf(grand.ID)
	if stg.EarnedL2 != 2500 || stg.Balance != 2500 {
		t.Fatalf("grand stats mismatch: %+v", stg)
	}

	// A second payment stacks on the live deadline (the "+30 days" rule).
	base := *p.ExpiresAt
	res2, err := s.RecordPayment(payer.ID, 10000, 30, "", 0)
	if err != nil {
		t.Fatalf("second payment: %v", err)
	}
	if !res2.NewExpiresAt.After(base) {
		t.Fatalf("second extension must stack: %v <= %v", res2.NewExpiresAt, base)
	}

	// days=0 records money without touching the deadline.
	before, _ := s.GetUser(payer.ID)
	if _, err := s.RecordPayment(payer.ID, 1000, 0, "no days", 0); err != nil {
		t.Fatalf("days=0 payment: %v", err)
	}
	after, _ := s.GetUser(payer.ID)
	if !after.ExpiresAt.Equal(*before.ExpiresAt) {
		t.Fatalf("days=0 must not move the deadline: %v vs %v", after.ExpiresAt, before.ExpiresAt)
	}

	// Organic payer: no earnings at all.
	loner := mustRefUser(t, s, "loner", nil)
	res3, err := s.RecordPayment(loner.ID, 50000, 0, "", 0)
	if err != nil {
		t.Fatalf("organic payment: %v", err)
	}
	if len(res3.Earnings) != 0 {
		t.Fatalf("organic payer must mint no earnings: %+v", res3.Earnings)
	}
}

func TestRecordPaymentRespectsSettings(t *testing.T) {
	s := openTestStore(t)
	if err := s.SetSetting(SettingReferralEnabled, "0"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := s.SetSetting(SettingReferralPercentL1, "50"); err != nil {
		t.Fatalf("set p1: %v", err)
	}

	ref := mustRefUser(t, s, "ref", nil)
	payer := mustRefUser(t, s, "payer", &ref.ID)

	res, err := s.RecordPayment(payer.ID, 10000, 0, "", 0)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if len(res.Earnings) != 0 {
		t.Fatalf("disabled program must mint nothing: %+v", res.Earnings)
	}

	if err := s.SetSetting(SettingReferralEnabled, "1"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	res, err = s.RecordPayment(payer.ID, 10000, 0, "", 0)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if len(res.Earnings) != 1 || res.Earnings[0].Amount != 5000 {
		t.Fatalf("custom percent mismatch: %+v", res.Earnings)
	}

	rs, err := s.ReferralSettings()
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if !rs.Enabled || rs.PercentL1 != 50 {
		t.Fatalf("resolved settings mismatch: %+v", rs)
	}
}

func TestReferralSettingsDefaults(t *testing.T) {
	s := openTestStore(t)
	rs, err := s.ReferralSettings()
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if !rs.Enabled || rs.PercentL1 != ReferralPercentL1Default || rs.PercentL2 != ReferralPercentL2Default {
		t.Fatalf("defaults mismatch: %+v", rs)
	}
}

func TestPayoutGuardAndBalance(t *testing.T) {
	s := openTestStore(t)
	ref := mustRefUser(t, s, "ref", nil)
	payer := mustRefUser(t, s, "payer", &ref.ID)
	if _, err := s.RecordPayment(payer.ID, 10000, 0, "", 0); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Overdraft is refused; the exact balance is fine and zeroes it out.
	if _, err := s.CreatePayout(ref.ID, 1501, "", 0); err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
	if _, err := s.CreatePayout(ref.ID, 1500, "usdt", 1); err != nil {
		t.Fatalf("payout: %v", err)
	}
	st, _ := s.ReferralStatsOf(ref.ID)
	if st.Balance != 0 || st.PaidOut != 1500 {
		t.Fatalf("post-payout stats mismatch: %+v", st)
	}

	payouts, err := s.ListPayouts(10)
	if err != nil || len(payouts) != 1 || payouts[0].Username != "ref" {
		t.Fatalf("payouts list: %+v err %v", payouts, err)
	}

	payments, err := s.ListPayments(10)
	if err != nil || len(payments) != 1 || payments[0].Amount != 10000 || payments[0].Username != "payer" {
		t.Fatalf("payments list: %+v err %v", payments, err)
	}
}

func TestReferralOverviewRows(t *testing.T) {
	s := openTestStore(t)
	grand := mustRefUser(t, s, "grand", nil)
	ref := mustRefUser(t, s, "ref", &grand.ID)
	payer := mustRefUser(t, s, "payer", &ref.ID)
	if _, err := s.RecordPayment(payer.ID, 20000, 0, "", 0); err != nil {
		t.Fatalf("record: %v", err)
	}

	rows, err := s.ReferralOverview()
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	byName := map[string]ReferralOverviewRow{}
	for _, r := range rows {
		byName[r.Username] = r
	}
	if r, ok := byName["ref"]; !ok || r.ReferrerName != "grand" || r.Invited != 1 || r.EarnedL1 != 3000 {
		t.Fatalf("ref row mismatch: %+v", r)
	}
	if r, ok := byName["grand"]; !ok || r.EarnedL2 != 1000 {
		t.Fatalf("grand row mismatch: %+v", r)
	}
	if r, ok := byName["payer"]; !ok || r.ReferrerName != "ref" {
		t.Fatalf("payer row mismatch: %+v", r)
	}
}
