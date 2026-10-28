package api

// Web-first sign-up: the web cabinet is the main door, Telegram is optional.
//
//	POST /api/auth/invite {invite_code, name?}
//	  -> {token, user, key, login_link}
//
// Opening an invite link (https://<domain>/cabinet/?invite=CODE) creates the
// account right away — no Telegram, no password, no BotFather /setdomain.
// The account gets a personal access key (the same credential the Android
// app signs in with, access_key.go); the cabinet keeps it in the browser
// and shows it as a "login link" (https://<domain>/cabinet/#k=KEY) to sign
// in again from another device. Rotating the key in the profile rotates
// the link. The invite stays one-time exactly like on the Telegram path.

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/fenjo26/Fibr/internal/store"
)

// LoginLink renders a personal web-cabinet login link for an access key.
// The key rides in the fragment: browsers never send it to the server in
// the request line or Referer, the cabinet reads it client-side.
func LoginLink(domain, key string) string {
	d := strings.TrimSuffix(strings.TrimSpace(domain), "/")
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if d == "" || d == "localhost" || strings.HasPrefix(d, "localhost:") {
		return "/cabinet/#k=" + key
	}
	return "https://" + d + "/cabinet/#k=" + key
}

func (a *API) handleAuthInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InviteCode string `json:"invite_code"`
		Name       string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	code := strings.TrimSpace(body.InviteCode)
	if code == "" {
		a.errJSON(w, http.StatusBadRequest, "invite required")
		return
	}
	inv, err := a.store.GetInvite(code)
	if err != nil || inv.UsedBy != nil {
		a.errJSON(w, http.StatusForbidden, "invalid invite")
		return
	}
	key, err := GenerateAccessKey()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot generate access key")
		return
	}
	name := sanitizeName(body.Name, "")
	if name == "" {
		name = sanitizeName(inv.Note, "")
	}
	u := &store.User{Username: name, Role: store.RoleUser}
	if err := a.store.CreateUser(u); err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot create user")
		return
	}
	if u.Username == "" {
		// A readable handle for the admin list until the person renames.
		u.Username = fmt.Sprintf("user-%d", u.ID)
		_ = a.store.SetUsername(u.ID, u.Username)
	}
	// Claim the code only after the account exists; a lost race rolls the
	// account back (one-time invite, same as the Telegram sign-in).
	if err := a.store.MarkUsed(code, u.ID); err != nil {
		_ = a.store.DeleteUser(u.ID)
		a.errJSON(w, http.StatusForbidden, "invalid invite")
		return
	}
	hash := hashAccessKey(key)
	if err := a.store.SetUserAccessToken(u.ID, &hash); err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot store access key")
		return
	}
	a.log.Info("user registered via web invite", "user_id", u.ID, "invite", code)
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      a.newSessionToken(u.ID),
		"user":       u,
		"key":        key,
		"login_link": LoginLink(a.cfg.Domain, key),
	})
}
