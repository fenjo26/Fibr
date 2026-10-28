package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fenjo26/Fibr/internal/config"
)

// signInitData builds initData the same way Telegram does, so we can verify
// our validation logic end to end.
func signInitData(botToken string, params map[string]string) string {
	vals := url.Values{}
	for k, v := range params {
		vals.Set(k, v)
	}
	keys := make([]string, 0, len(params))
	for k := range params {
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
		b.WriteString(params[k])
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(b.String()))
	vals.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return vals.Encode()
}

func TestValidateInitData(t *testing.T) {
	const botToken = "7000000001:TEST-TOKEN"
	params := map[string]string{
		"auth_date": strconv.FormatInt(time.Now().Unix(), 10),
		"query_id":  "AAHdF6IQAAAAAN0XohDhrOrc",
		"user":      `{"id":42,"first_name":"Ivan","username":"ivan","language_code":"ru"}`,
	}
	t.Run("valid", func(t *testing.T) {
		u, err := ValidateInitData(signInitData(botToken, params), botToken)
		if err != nil {
			t.Fatalf("expected valid, got error: %v", err)
		}
		if u.ID != 42 || u.Username != "ivan" || u.LanguageCode != "ru" {
			t.Fatalf("parsed user mismatch: %+v", u)
		}
	})
	t.Run("wrong bot token", func(t *testing.T) {
		if _, err := ValidateInitData(signInitData(botToken, params), "other:token"); err == nil {
			t.Fatal("expected signature error for wrong bot token")
		}
	})
	t.Run("tampered payload", func(t *testing.T) {
		initData := signInitData(botToken, params)
		vals, _ := url.ParseQuery(initData)
		vals.Set("user", `{"id":999,"first_name":"Hacker"}`)
		if _, err := ValidateInitData(vals.Encode(), botToken); err == nil {
			t.Fatal("expected signature error for tampered payload")
		}
	})
	t.Run("stale", func(t *testing.T) {
		old := map[string]string{}
		for k, v := range params {
			old[k] = v
		}
		old["auth_date"] = strconv.FormatInt(time.Now().Add(-25*time.Hour).Unix(), 10)
		if _, err := ValidateInitData(signInitData(botToken, old), botToken); err == nil {
			t.Fatal("expected staleness error")
		}
	})
	t.Run("missing hash", func(t *testing.T) {
		if _, err := ValidateInitData("auth_date=1&user=%7B%22id%22%3A1%7D", botToken); err == nil {
			t.Fatal("expected error for missing hash")
		}
	})
}

func TestSessionToken(t *testing.T) {
	a := &API{cfg: &config.Config{Secret: "unit-test-secret"}}

	tok := a.newSessionToken(7)
	uid, ok := a.parseSessionToken(tok)
	if !ok || uid != 7 {
		t.Fatalf("expected uid 7, got %d ok=%v", uid, ok)
	}

	if _, ok := a.parseSessionToken(tok + "x"); ok {
		t.Fatal("tampered token must not validate")
	}
	if _, ok := a.parseSessionToken("7.99999999999." + strings.Repeat("0", 64)); ok {
		t.Fatal("token with foreign signature must not validate")
	}

	// Expired token: craft with negative expiry.
	exp := time.Now().Add(-time.Hour).Unix()
	msg := "7|" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte("unit-test-secret"))
	mac.Write([]byte(msg))
	stale := "7." + strconv.FormatInt(exp, 10) + "." + hex.EncodeToString(mac.Sum(nil))
	if _, ok := a.parseSessionToken(stale); ok {
		t.Fatal("expired token must not validate")
	}
}

// signLoginWidget builds Telegram Login Widget data the way Telegram does:
// flat user fields + hash = HMAC_SHA256(data_check_string, SHA256(token)).
func signLoginWidget(botToken string, params map[string]string) string {
	vals := url.Values{}
	keys := make([]string, 0, len(params))
	for k := range params {
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
		b.WriteString(params[k])
	}
	secret := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(b.String()))
	// The query carries the signed fields too — only encoding the hash would
	// leave ValidateLoginWidget with nothing to verify (and no auth_date).
	for k, v := range params {
		vals.Set(k, v)
	}
	vals.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return vals.Encode()
}

func TestValidateLoginWidget(t *testing.T) {
	const botToken = "7000000002:WIDGET-TOKEN"
	params := map[string]string{
		"id":         "77",
		"first_name": "Oleg",
		"username":   "vpnoleg",
		"auth_date":  strconv.FormatInt(time.Now().Unix(), 10),
	}

	t.Run("valid", func(t *testing.T) {
		u, err := ValidateLoginWidget(signLoginWidget(botToken, params), botToken)
		if err != nil {
			t.Fatalf("expected valid, got error: %v", err)
		}
		if u.ID != 77 || u.Username != "vpnoleg" || u.FirstName != "Oleg" {
			t.Fatalf("parsed user mismatch: %+v", u)
		}
	})
	t.Run("wrong token", func(t *testing.T) {
		if _, err := ValidateLoginWidget(signLoginWidget(botToken, params), "other"); err == nil {
			t.Fatal("expected signature error for wrong bot token")
		}
	})
	t.Run("tampered", func(t *testing.T) {
		q := signLoginWidget(botToken, params)
		vals, _ := url.ParseQuery(q)
		vals.Set("id", "999")
		if _, err := ValidateLoginWidget(vals.Encode(), botToken); err == nil {
			t.Fatal("expected signature error for tampered id")
		}
	})
	t.Run("stale", func(t *testing.T) {
		old := map[string]string{}
		for k, v := range params {
			old[k] = v
		}
		old["auth_date"] = strconv.FormatInt(time.Now().Add(-25*time.Hour).Unix(), 10)
		if _, err := ValidateLoginWidget(signLoginWidget(botToken, old), botToken); err == nil {
			t.Fatal("expected staleness error")
		}
	})
}
