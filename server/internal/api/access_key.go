package api

// App access keys: a personal credential for the Flutter client's server
// login ("этап 2.5"). The user generates a 32-char key in the Mini App /
// web cabinet profile ("Ключ для приложения"), types it into the app with
// the server domain, and receives the same {token, user} session the
// Telegram sign-in issues. Only the SHA-256 hash is stored; the plaintext
// is returned by POST /api/me/access-key exactly once (a repeat call
// rotates the key). A key grants its OWNER's rights — there is no separate
// key role.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"net/http"
	"strings"

	"github.com/fenjo26/Fibr/internal/store"
)

// accessKeyLen is 32 chars over the 53-char invite alphabet (53^32 ≈ 1e55
// combinations) — long enough to resist guessing over the network forever,
// short enough to retype from another screen.
const accessKeyLen = 32

// GenerateAccessKey returns a crypto/rand key of accessKeyLen chars from
// inviteAlphabet (same look-alike-free alphabet as invite codes).
func GenerateAccessKey() (string, error) {
	var b strings.Builder
	b.Grow(accessKeyLen)
	max := big.NewInt(int64(len(inviteAlphabet)))
	for i := 0; i < accessKeyLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(inviteAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// hashAccessKey is the storage form of an access key: SHA-256 hex. Logins
// and identify() compare hashes, never the keys themselves.
func hashAccessKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// handleMeAccessKeyCreate issues (or rotates) the caller's app access key.
// The plaintext key is returned exactly once; a repeat call replaces the
// old key, which stops working immediately. There is no way to re-show an
// existing key — the user rotates instead.
func (a *API) handleMeAccessKeyCreate(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	had, err := a.store.HasUserAccessToken(id.userID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	key, err := GenerateAccessKey()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot generate access key")
		return
	}
	hash := hashAccessKey(key)
	if err := a.store.SetUserAccessToken(id.userID, &hash); err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot store access key")
		return
	}
	a.log.Info("app access key issued", "user_id", id.userID, "rotation", had)
	writeJSON(w, http.StatusCreated, map[string]string{"key": key})
}

// handleMeAccessKeyDelete revokes the caller's app access key (the account
// stays; key-based sign-ins stop working).
func (a *API) handleMeAccessKeyDelete(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	if err := a.store.SetUserAccessToken(id.userID, nil); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.errJSON(w, http.StatusNotFound, "user not found")
			return
		}
		a.errJSON(w, http.StatusInternalServerError, "cannot revoke access key")
		return
	}
	a.log.Info("app access key revoked", "user_id", id.userID)
	w.WriteHeader(http.StatusNoContent)
}

// handleAuthKey signs a user in with an app access key: the same response
// shape as the Telegram sign-in ({token, user}). The key is resolved by its
// SHA-256 hash; disabled and expired accounts are refused the same way.
func (a *API) handleAuthKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	key := strings.TrimSpace(body.Key)
	if key == "" {
		a.errJSON(w, http.StatusBadRequest, "key required")
		return
	}
	u, err := a.store.GetUserByAccessTokenHash(hashAccessKey(key))
	if err != nil {
		a.errJSON(w, http.StatusUnauthorized, "invalid key")
		return
	}
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
