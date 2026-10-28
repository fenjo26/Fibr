package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// InitUser is the Telegram user embedded in Mini App initData.
type InitUser struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	LanguageCode string `json:"language_code"`
}

var (
	errBadInitData   = errors.New("invalid initData signature")
	errStaleInitData = errors.New("initData is too old")
)

// ValidateInitData validates Telegram Mini App initData and returns the user.
// Algorithm: https://core.telegram.org/bots/webapps#validating-data-received-via-the-mini-app
func ValidateInitData(initData, botToken string) (InitUser, error) {
	var zero InitUser
	if initData == "" || botToken == "" {
		return zero, errors.New("initData or bot token is empty")
	}
	vals, err := url.ParseQuery(initData)
	if err != nil {
		return zero, fmt.Errorf("parse initData: %w", err)
	}
	hash := vals.Get("hash")
	if hash == "" {
		return zero, errors.New("initData: missing hash")
	}
	authDate, err := strconv.ParseInt(vals.Get("auth_date"), 10, 64)
	if err != nil || authDate == 0 {
		return zero, errors.New("initData: missing auth_date")
	}
	if time.Since(time.Unix(authDate, 0)) > 24*time.Hour {
		return zero, errStaleInitData
	}

	// data-check-string: every field except hash/signature, sorted by key,
	// joined as "key=value" with newline separators.
	keys := make([]string, 0, len(vals))
	for k := range vals {
		if k == "hash" || k == "signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(vals.Get(k))
	}

	// secret = HMAC_SHA256(bot_token, key="WebAppData")
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	// expected = HMAC_SHA256(data_check_string, key=secret)
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(b.String()))
	if !strings.EqualFold(hex.EncodeToString(mac.Sum(nil)), hash) {
		return zero, errBadInitData
	}

	rawUser := vals.Get("user")
	if rawUser == "" {
		return zero, errors.New("initData: missing user")
	}
	var u InitUser
	if err := json.Unmarshal([]byte(rawUser), &u); err != nil {
		return zero, fmt.Errorf("initData: bad user payload: %w", err)
	}
	if u.ID == 0 {
		return zero, errors.New("initData: user has no id")
	}
	return u, nil
}

// ValidateLoginWidget verifies data from the Telegram Login Widget
// (https://core.telegram.org/widgets/login) — the browser twin of the Mini
// App used by the web cabinet at /cabinet/. Same data-check-string idea, but
// the secret is plain SHA256(bot_token) and the user fields arrive flat
// (id=…&first_name=…&hash=…) instead of a JSON "user" param.
func ValidateLoginWidget(query, botToken string) (InitUser, error) {
	var zero InitUser
	if query == "" || botToken == "" {
		return zero, errors.New("login widget: empty data or token")
	}
	vals, err := url.ParseQuery(query)
	if err != nil {
		return zero, fmt.Errorf("login widget: parse: %w", err)
	}
	hash := vals.Get("hash")
	if hash == "" {
		return zero, errors.New("login widget: missing hash")
	}
	authDate, err := strconv.ParseInt(vals.Get("auth_date"), 10, 64)
	if err != nil || authDate == 0 {
		return zero, errors.New("login widget: missing auth_date")
	}
	if time.Since(time.Unix(authDate, 0)) > 24*time.Hour {
		return zero, errStaleInitData
	}

	keys := make([]string, 0, len(vals))
	for k := range vals {
		if k == "hash" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(vals.Get(k))
	}

	// secret = SHA256(bot_token) for the login widget.
	secret := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(b.String()))
	if !strings.EqualFold(hex.EncodeToString(mac.Sum(nil)), hash) {
		return zero, errBadInitData
	}

	id, err := strconv.ParseInt(vals.Get("id"), 10, 64)
	if err != nil || id == 0 {
		return zero, errors.New("login widget: missing user id")
	}
	return InitUser{
		ID:           id,
		Username:     vals.Get("username"),
		FirstName:    vals.Get("first_name"),
		LastName:     vals.Get("last_name"),
		LanguageCode: vals.Get("language_code"),
	}, nil
}

// newSessionToken issues a "uid.exp.signature" HMAC token valid for 30 days.
func (a *API) newSessionToken(userID int64) string {
	exp := time.Now().Add(30 * 24 * time.Hour).Unix()
	msg := fmt.Sprintf("%d|%d", userID, exp)
	return fmt.Sprintf("%d.%d.%s", userID, exp, hexHMAC(a.cfg.Secret, msg))
}

// parseSessionToken verifies a session token and returns the user id.
func (a *API) parseSessionToken(token string) (int64, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, false
	}
	uid, err1 := strconv.ParseInt(parts[0], 10, 64)
	exp, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	if time.Now().Unix() > exp {
		return 0, false
	}
	want := hexHMAC(a.cfg.Secret, fmt.Sprintf("%d|%d", uid, exp))
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return 0, false
	}
	return uid, true
}

func hexHMAC(secret, msg string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}
