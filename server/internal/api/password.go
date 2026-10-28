package api

// Login + password for the web cabinet (and anything else that signs in
// through /api/auth/password). Flow for non-technical people: an invite
// link (or a code the admin hands out) gets them in; in the profile they
// set their own login and password and use those from then on.
//
// Passwords are stored as salted PBKDF2-SHA256 (stdlib crypto/pbkdf2,
// 600k iterations — OWASP 2023 guidance), never in plaintext. Failed
// sign-ins are rate-limited per login.

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fenjo26/Fibr/internal/store"
)

const (
	pbkdf2Iter      = 600_000
	minPasswordLen  = 8
	maxPasswordLen  = 128
	loginFailWindow = 15 * time.Minute
	loginFailMax    = 10
)

var loginRe = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

// normLogin lower-cases and validates a login; "" when invalid.
func normLogin(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if !loginRe.MatchString(s) {
		return ""
	}
	return s
}

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter, hex.EncodeToString(salt), hex.EncodeToString(dk)), nil
}

func verifyPassword(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err1 := hex.DecodeString(parts[2])
	want, err2 := hex.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// loginLimiter counts failed password sign-ins per login.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

func (l *loginLimiter) blocked(login string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.fails[login][:0]
	for _, t := range l.fails[login] {
		if now.Sub(t) < loginFailWindow {
			recent = append(recent, t)
		}
	}
	if l.fails == nil {
		l.fails = map[string][]time.Time{}
	}
	l.fails[login] = recent
	return len(recent) >= loginFailMax
}

func (l *loginLimiter) fail(login string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fails == nil {
		l.fails = map[string][]time.Time{}
	}
	l.fails[login] = append(l.fails[login], now)
}

func (l *loginLimiter) reset(login string) {
	l.mu.Lock()
	delete(l.fails, login)
	l.mu.Unlock()
}

// handleMeSetPassword (signed in): set or change the account's own login
// and password.
func (a *API) handleMeSetPassword(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	login := normLogin(body.Login)
	if login == "" {
		a.errJSON(w, http.StatusBadRequest, "login: 3-32 chars, latin letters, digits, . _ -")
		return
	}
	if n := len([]rune(body.Password)); n < minPasswordLen || n > maxPasswordLen {
		a.errJSON(w, http.StatusBadRequest, fmt.Sprintf("password: %d-%d characters", minPasswordLen, maxPasswordLen))
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot hash password")
		return
	}
	switch err := a.store.SetUserLogin(id.userID, login, hash); {
	case errors.Is(err, store.ErrLoginTaken):
		a.errJSON(w, http.StatusConflict, "login taken")
		return
	case err != nil:
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	a.log.Info("web login set", "user_id", id.userID)
	writeJSON(w, http.StatusOK, map[string]any{"login": login})
}

// handleAuthPassword signs in with login + password ({token, user}).
func (a *API) handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	login := normLogin(body.Login)
	now := time.Now()
	if login == "" || body.Password == "" {
		a.errJSON(w, http.StatusUnauthorized, "wrong login or password")
		return
	}
	if a.loginFails.blocked(login, now) {
		a.errJSON(w, http.StatusTooManyRequests, "too many attempts, try again in 15 minutes")
		return
	}
	u, hash, err := a.store.GetUserByLogin(login)
	if err != nil || hash == "" || !verifyPassword(body.Password, hash) {
		a.loginFails.fail(login, now)
		a.errJSON(w, http.StatusUnauthorized, "wrong login or password")
		return
	}
	a.loginFails.reset(login)
	if u.Disabled {
		a.errJSON(w, http.StatusForbidden, "account disabled")
		return
	}
	if a.userExpired(u) {
		a.errJSON(w, http.StatusForbidden, msgSubscriptionExpired)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": a.newSessionToken(u.ID), "user": u})
}

// handleAdminUserAccessKey (admin): issue a personal sign-in code for one
// of the caller's users — "give the person a code like the admin token".
// The code is shown once (and as a login link); a repeat call rotates it.
func (a *API) handleAdminUserAccessKey(w http.ResponseWriter, r *http.Request) {
	u, _, ok := a.fetchManagedUser(w, r)
	if !ok {
		return
	}
	key, err := GenerateAccessKey()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot generate access key")
		return
	}
	hash := hashAccessKey(key)
	if err := a.store.SetUserAccessToken(u.ID, &hash); err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot store access key")
		return
	}
	a.log.Info("access key issued by admin", "user_id", u.ID, "by", ident(r).userID)
	writeJSON(w, http.StatusCreated, map[string]any{"key": key, "login_link": LoginLink(a.cfg.Domain, key)})
}
