package store

// Referral program persistence (see docs/referrals.md): permanent per-user
// referral codes, attribution (users.referred_by), a manual payment ledger
// (the superadmin records what a user paid), two-level earnings derived from
// those payments, and manual payouts. All money is minor units (kopecks) in
// INTEGER columns — the same "money stays outside the system" philosophy as
// the expiry billing: the ledger only records what actually happened.

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"strings"
	"time"
)

// ErrInsufficientBalance is returned by CreatePayout when the requested
// amount exceeds the user's current referral balance.
var ErrInsufficientBalance = errors.New("insufficient referral balance")

// referralAlphabet mirrors the invite alphabet (URL-safe, look-alikes
// removed); codes are shorter than invites because they are copy-pasted,
// never read aloud by the superadmin.
const (
	referralAlphabet = "23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ"
	referralCodeLen  = 8
)

func genReferralCode() (string, error) {
	var b strings.Builder
	b.Grow(referralCodeLen)
	max := big.NewInt(int64(len(referralAlphabet)))
	for i := 0; i < referralCodeLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(referralAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// backfillReferralCodes mints codes for accounts created before the referral
// program existed (referral_code IS NULL). Idempotent by construction; runs
// once at Open() right after the column migration.
func (s *Store) backfillReferralCodes() error {
	rows, err := s.db.Query(`SELECT id FROM users WHERE referral_code IS NULL`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range ids {
		var lastErr error
		for attempt := 0; attempt < 5; attempt++ {
			code, err := genReferralCode()
			if err != nil {
				return err
			}
			if _, err := s.db.Exec(`UPDATE users SET referral_code = ? WHERE id = ? AND referral_code IS NULL`, code, id); err == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
			}
		}
		if lastErr != nil {
			return lastErr
		}
	}
	return nil
}

// GetUserByReferralCode resolves a referral code to its owner, or ErrNotFound.
func (s *Store) GetUserByReferralCode(code string) (*User, error) {
	row := s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE referral_code = ?`, code)
	return scanUser(row)
}

/* ------------------------- referral settings ------------------------- */

// ReferralSettings is the resolved program configuration (settings keys with
// defaults applied for absent keys).
type ReferralSettings struct {
	Enabled     bool
	PercentL1   int64 // % of a payment to the direct referrer
	PercentL2   int64 // % of a payment to the referrer's referrer
	Currency    string
	ContactInfo string
}

// ReferralSettings loads the program configuration. Absent keys read as the
// documented defaults (enabled, 15/5, no currency suffix, no contact line).
func (s *Store) ReferralSettings() (ReferralSettings, error) {
	var rs ReferralSettings
	if v, err := s.GetSetting(SettingReferralEnabled); err == nil {
		rs.Enabled = v == "1"
	} else if !errors.Is(err, ErrNotFound) {
		return rs, err
	} else {
		rs.Enabled = true // absent = default ON
	}
	p1, err := s.GetSettingInt(SettingReferralPercentL1)
	if err != nil {
		return rs, err
	}
	if p1 == 0 {
		p1 = ReferralPercentL1Default
	}
	rs.PercentL1 = p1
	p2, err := s.GetSettingInt(SettingReferralPercentL2)
	if err != nil {
		return rs, err
	}
	if p2 == 0 {
		p2 = ReferralPercentL2Default
	}
	rs.PercentL2 = p2
	if rs.Currency, err = s.GetSetting(SettingCurrency); err != nil && !errors.Is(err, ErrNotFound) {
		return rs, err
	}
	if rs.ContactInfo, err = s.GetSetting(SettingContactInfo); err != nil && !errors.Is(err, ErrNotFound) {
		return rs, err
	}
	return rs, nil
}

/* ---------------------------- payments ---------------------------- */

// Payment is one recorded top-up: the superadmin enters what the user paid
// (Amount, minor units) and optionally how many subscription days that buys.
type Payment struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Amount    int64     `json:"amount"` // minor units, > 0
	Days      int64     `json:"days"`   // subscription days granted (0 = none)
	Note      string    `json:"note"`
	CreatedBy int64     `json:"created_by"` // 0 = static admin token
	CreatedAt time.Time `json:"created_at"`
}

// PaymentWithUser is a payment joined with the paying user's display name
// for the admin ledger tables.
type PaymentWithUser struct {
	Payment
	Username string `json:"username"`
}

// ReferralEarning is a commission credited to a beneficiary from someone
// else's payment. Level 1 = the payer's direct referrer, level 2 = theirs.
type ReferralEarning struct {
	ID            int64     `json:"id"`
	BeneficiaryID int64     `json:"beneficiary_id"`
	PaymentID     int64     `json:"payment_id"`
	PayerID       int64     `json:"payer_id"`
	Level         int       `json:"level"`
	Amount        int64     `json:"amount"` // minor units
	CreatedAt     time.Time `json:"created_at"`
}

// PaymentResult reports what one recorded payment did: the ledger row, the
// new subscription deadline (nil = unchanged) and the commissions minted.
type PaymentResult struct {
	Payment      Payment           `json:"payment"`
	NewExpiresAt *time.Time        `json:"new_expires_at"`
	Earnings     []ReferralEarning `json:"earnings"`
}

// RecordPayment writes a top-up and, in the same transaction, extends the
// subscription (days > 0, from max(now, current deadline) — same arithmetic
// as the panel's "+30 days" button) and credits referral commissions per the
// current settings. Attribution is read inside the transaction so a payment
// and the referring chain are consistent.
func (s *Store) RecordPayment(userID, amount, days int64, note string, createdBy int64) (*PaymentResult, error) {
	if amount <= 0 {
		return nil, errors.New("payment amount must be positive")
	}
	if days < 0 {
		return nil, errors.New("payment days must not be negative")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now()

	// Program settings, resolved in-transaction (the pool has a single
	// connection; going through s.* here would self-deadlock).
	var enabled bool
	var p1, p2 int64
	var v sql.NullString
	if err := tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, SettingReferralEnabled).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	enabled = !v.Valid || v.String == "1" // absent = default ON
	if err := tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, SettingReferralPercentL1).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	p1 = ReferralPercentL1Default
	if v.Valid {
		if n, ok := parseInt64(v.String); ok && n >= 0 {
			p1 = n
		}
	}
	if err := tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, SettingReferralPercentL2).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	p2 = ReferralPercentL2Default
	if v.Valid {
		if n, ok := parseInt64(v.String); ok && n >= 0 {
			p2 = n
		}
	}

	res := &PaymentResult{Payment: Payment{UserID: userID, Amount: amount, Days: days, Note: note, CreatedBy: createdBy, CreatedAt: now}}
	if err := tx.QueryRow(`SELECT id FROM users WHERE id = ?`, userID).Scan(new(int64)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ins, err := tx.Exec(
		`INSERT INTO payments (user_id, amount, days, note, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, amount, days, note, createdBy, now.Unix(),
	)
	if err != nil {
		return nil, err
	}
	res.Payment.ID, err = ins.LastInsertId()
	if err != nil {
		return nil, err
	}

	// Subscription extension: base = max(now, current deadline), nil stays a
	// concrete date only when days > 0 (a free-tier "unlimited" is never
	// downgraded by a payment).
	if days > 0 {
		var exp sql.NullInt64
		if err := tx.QueryRow(`SELECT expires_at FROM users WHERE id = ?`, userID).Scan(&exp); err != nil {
			return nil, err
		}
		base := now.Unix()
		if exp.Valid && exp.Int64 > base {
			base = exp.Int64
		}
		next := base + days*86400
		if _, err := tx.Exec(`UPDATE users SET expires_at = ? WHERE id = ?`, next, userID); err != nil {
			return nil, err
		}
		t := time.Unix(next, 0)
		res.NewExpiresAt = &t
	}

	// Two-level commissions along the attribution chain: payer -> referrer
	// (L1) -> referrer's referrer (L2). Floor division on minor units.
	if enabled {
		var l1 sql.NullInt64
		if err := tx.QueryRow(`SELECT referred_by FROM users WHERE id = ?`, userID).Scan(&l1); err != nil {
			return nil, err
		}
		if l1.Valid && p1 > 0 {
			amt := amount * p1 / 100
			e := ReferralEarning{BeneficiaryID: l1.Int64, PaymentID: res.Payment.ID, PayerID: userID, Level: 1, Amount: amt, CreatedAt: now}
			if err := insertEarning(tx, &e); err != nil {
				return nil, err
			}
			res.Earnings = append(res.Earnings, e)
			var l2 sql.NullInt64
			if err := tx.QueryRow(`SELECT referred_by FROM users WHERE id = ?`, l1.Int64).Scan(&l2); err != nil {
				return nil, err
			}
			if l2.Valid && l2.Int64 != userID && p2 > 0 {
				amt := amount * p2 / 100
				e := ReferralEarning{BeneficiaryID: l2.Int64, PaymentID: res.Payment.ID, PayerID: userID, Level: 2, Amount: amt, CreatedAt: now}
				if err := insertEarning(tx, &e); err != nil {
					return nil, err
				}
				res.Earnings = append(res.Earnings, e)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

func insertEarning(tx *sql.Tx, e *ReferralEarning) error {
	if e.Amount <= 0 {
		return nil // a 1-kopeck payment at 5% floors to zero; nothing to credit
	}
	ins, err := tx.Exec(
		`INSERT INTO referral_earnings (beneficiary_id, payment_id, payer_id, level, amount, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		e.BeneficiaryID, e.PaymentID, e.PayerID, e.Level, e.Amount, e.CreatedAt.Unix(),
	)
	if err != nil {
		return err
	}
	e.ID, err = ins.LastInsertId()
	return err
}

func parseInt64(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, true
}

// ListPayments returns the most recent payments with the paying user's name.
func (s *Store) ListPayments(limit int) ([]PaymentWithUser, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT p.id, p.user_id, p.amount, p.days, p.note, p.created_by, p.created_at, u.username
		 FROM payments p JOIN users u ON u.id = p.user_id ORDER BY p.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PaymentWithUser
	for rows.Next() {
		var pw PaymentWithUser
		var created int64
		if err := rows.Scan(&pw.ID, &pw.UserID, &pw.Amount, &pw.Days, &pw.Note, &pw.CreatedBy, &created, &pw.Username); err != nil {
			return nil, err
		}
		pw.CreatedAt = time.Unix(created, 0)
		out = append(out, pw)
	}
	return out, rows.Err()
}

/* ---------------------------- stats / overview ---------------------------- */

// ReferralStats aggregates one user's referral program position.
type ReferralStats struct {
	Invited  int64 `json:"invited"`   // direct referrals (L1)
	EarnedL1 int64 `json:"earned_l1"` // minor units
	EarnedL2 int64 `json:"earned_l2"` // minor units
	PaidOut  int64 `json:"paid_out"`  // minor units
	Balance  int64 `json:"balance"`   // earned L1+L2 - paid out
}

// ReferralStatsOf computes a user's aggregates with three scalar queries.
func (s *Store) ReferralStatsOf(userID int64) (ReferralStats, error) {
	var st ReferralStats
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE referred_by = ?`, userID).Scan(&st.Invited); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN level = 1 THEN amount ELSE 0 END), 0), COALESCE(SUM(CASE WHEN level = 2 THEN amount ELSE 0 END), 0) FROM referral_earnings WHERE beneficiary_id = ?`, userID).Scan(&st.EarnedL1, &st.EarnedL2); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM referral_payouts WHERE user_id = ?`, userID).Scan(&st.PaidOut); err != nil {
		return st, err
	}
	st.Balance = st.EarnedL1 + st.EarnedL2 - st.PaidOut
	return st, nil
}

// ReferralEntry is one invited account in a referrer's list, with the L1
// commission that account has generated so far.
type ReferralEntry struct {
	ID         int64     `json:"id"`
	Username   string    `json:"username"`
	TelegramID *int64    `json:"telegram_id"`
	Disabled   bool      `json:"disabled"`
	CreatedAt  time.Time `json:"created_at"`
	Earned     int64     `json:"earned"` // L1 commissions from this account, minor units
}

// ListReferralsOf returns the accounts that signed up with userID's code,
// newest first.
func (s *Store) ListReferralsOf(userID int64) ([]ReferralEntry, error) {
	rows, err := s.db.Query(
		`SELECT u.id, u.username, u.telegram_id, u.disabled, u.created_at,
			COALESCE((SELECT SUM(e.amount) FROM referral_earnings e WHERE e.payer_id = u.id AND e.beneficiary_id = ? AND e.level = 1), 0)
		 FROM users u WHERE u.referred_by = ? ORDER BY u.id DESC`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReferralEntry
	for rows.Next() {
		var e ReferralEntry
		var tid sql.NullInt64
		var created int64
		var dis int
		if err := rows.Scan(&e.ID, &e.Username, &tid, &dis, &created, &e.Earned); err != nil {
			return nil, err
		}
		if tid.Valid {
			t := tid.Int64
			e.TelegramID = &t
		}
		e.Disabled = dis != 0
		e.CreatedAt = time.Unix(created, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReferralOverviewRow is one row of the superadmin's referral table: an
// account that takes part in the program on either side.
type ReferralOverviewRow struct {
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	ReferrerName string `json:"referrer_name"` // whose code this account used ("" = organic)
	ReferrerID   *int64 `json:"referrer_id"`
	ReferralStats
}

// ReferralOverview builds the superadmin's referral view: every account with
// an attribution, an earning or a payout, plus each account's aggregates.
// Joins are done in memory from ListUsers — the user count is panel-scale.
func (s *Store) ReferralOverview() ([]ReferralOverviewRow, error) {
	users, err := s.ListUsers()
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*User, len(users))
	for i := range users {
		byID[users[i].ID] = &users[i]
	}
	earn := make(map[int64]*ReferralStats)
	for _, u := range users {
		earn[u.ID] = &ReferralStats{}
	}
	rows, err := s.db.Query(`SELECT beneficiary_id, level, SUM(amount) FROM referral_earnings GROUP BY beneficiary_id, level`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var bid, sum int64
		var level int
		if err := rows.Scan(&bid, &level, &sum); err != nil {
			rows.Close()
			return nil, err
		}
		if st := earn[bid]; st != nil {
			if level == 1 {
				st.EarnedL1 = sum
			} else {
				st.EarnedL2 = sum
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = s.db.Query(`SELECT user_id, SUM(amount) FROM referral_payouts GROUP BY user_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var uid, sum int64
		if err := rows.Scan(&uid, &sum); err != nil {
			rows.Close()
			return nil, err
		}
		if st := earn[uid]; st != nil {
			st.PaidOut = sum
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = s.db.Query(`SELECT referred_by, COUNT(*) FROM users WHERE referred_by IS NOT NULL GROUP BY referred_by`)
	if err != nil {
		return nil, err
	}
	invited := map[int64]int64{}
	for rows.Next() {
		var rid, n int64
		if err := rows.Scan(&rid, &n); err != nil {
			rows.Close()
			return nil, err
		}
		invited[rid] = n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var out []ReferralOverviewRow
	for _, u := range users {
		st := earn[u.ID]
		st.Invited = invited[u.ID]
		st.Balance = st.EarnedL1 + st.EarnedL2 - st.PaidOut
		// Keep the table to accounts the program actually touches: invited
		// someone, were invited, earned or got paid.
		if u.ReferredBy == nil && st.Invited == 0 && st.EarnedL1 == 0 && st.EarnedL2 == 0 && st.PaidOut == 0 {
			continue
		}
		row := ReferralOverviewRow{UserID: u.ID, Username: u.Username, ReferrerID: u.ReferredBy, ReferralStats: *st}
		if u.ReferredBy != nil {
			if ref := byID[*u.ReferredBy]; ref != nil {
				row.ReferrerName = ref.Username
			}
		}
		out = append(out, row)
	}
	return out, nil
}

/* ---------------------------- payouts ---------------------------- */

// ReferralPayout records money the superadmin handed to a user outside the
// system (crypto, transfer — see docs/billing.md) and subtracts it from the
// user's balance.
type ReferralPayout struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Amount    int64     `json:"amount"` // minor units, > 0
	Note      string    `json:"note"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// PayoutWithUser is a payout joined with the recipient's display name.
type PayoutWithUser struct {
	ReferralPayout
	Username string `json:"username"`
}

// CreatePayout records a manual payout. The amount may not exceed the
// user's current balance (an over-payout would silently flip the balance
// negative); corrections go through a new earning note, not overdrafts.
func (s *Store) CreatePayout(userID, amount int64, note string, createdBy int64) (*ReferralPayout, error) {
	if amount <= 0 {
		return nil, errors.New("payout amount must be positive")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var earned int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM referral_earnings WHERE beneficiary_id = ?`, userID).Scan(&earned); err != nil {
		return nil, err
	}
	var paid int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM referral_payouts WHERE user_id = ?`, userID).Scan(&paid); err != nil {
		return nil, err
	}
	if earned-paid < amount {
		return nil, ErrInsufficientBalance
	}
	now := time.Now()
	p := &ReferralPayout{UserID: userID, Amount: amount, Note: note, CreatedBy: createdBy, CreatedAt: now}
	ins, err := tx.Exec(
		`INSERT INTO referral_payouts (user_id, amount, note, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, amount, note, createdBy, now.Unix())
	if err != nil {
		return nil, err
	}
	if p.ID, err = ins.LastInsertId(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

// ListPayouts returns the most recent payouts with the recipient's name.
func (s *Store) ListPayouts(limit int) ([]PayoutWithUser, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT p.id, p.user_id, p.amount, p.note, p.created_by, p.created_at, u.username
		 FROM referral_payouts p JOIN users u ON u.id = p.user_id ORDER BY p.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayoutWithUser
	for rows.Next() {
		var pw PayoutWithUser
		var created int64
		if err := rows.Scan(&pw.ID, &pw.UserID, &pw.Amount, &pw.Note, &pw.CreatedBy, &created, &pw.Username); err != nil {
			return nil, err
		}
		pw.CreatedAt = time.Unix(created, 0)
		out = append(out, pw)
	}
	return out, rows.Err()
}
