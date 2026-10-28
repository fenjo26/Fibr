// Package api serves the Fibr REST API consumed by the admin panel, the
// Telegram Mini App and the bot.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/store"
	"github.com/fenjo26/Fibr/internal/wg"
	"github.com/fenjo26/Fibr/internal/xray"
	"github.com/skip2/go-qrcode"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// API bundles the HTTP handlers.
type API struct {
	cfg         *config.Config
	store       *store.Store
	wg          *wg.Manager
	xray        *xray.Manager
	log         *slog.Logger
	botUsername atomic.Value // string, "" until the bot reports getMe
	updaterURL  string       // opt-in update sidecar; "" = never configured
}

// New creates the API.
func New(cfg *config.Config, st *store.Store, wgm *wg.Manager, xm *xray.Manager, log *slog.Logger) *API {
	a := &API{cfg: cfg, store: st, wg: wgm, xray: xm, log: log}
	a.botUsername.Store("")
	// TODO: move to config (internal/config is intentionally untouched here).
	a.updaterURL = os.Getenv("FIBR_UPDATER_URL")
	if a.updaterURL == "" {
		a.updaterURL = "http://updater:8912"
	}
	return a
}

// SetBotUsername records the bot @username (called from main after getMe) so
// /api/public/info can render the Telegram Login Widget in the web cabinet.
func (a *API) SetBotUsername(name string) {
	a.botUsername.Store(name)
}

// botName is the bot @username ("" until the bot reported getMe).
func (a *API) botName() string {
	name, _ := a.botUsername.Load().(string)
	return name
}

// Handler builds the routed, panic-safe HTTP handler.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/public/info", a.handlePublicInfo)
	mux.HandleFunc("POST /api/auth/telegram", a.handleAuthTelegram)
	// Server login for the Flutter client: exchange an app access key for
	// the same {token, user} session the Telegram sign-in issues.
	mux.HandleFunc("POST /api/auth/key", a.handleAuthKey)

	// Quick-connect: public exchange of a one-time code for a device config,
	// plus the /join/<code> landing page (never consumes the code, so
	// messenger link previews cannot burn it). See docs/join.md.
	mux.HandleFunc("GET /join/{code}", a.handleJoinLanding)
	mux.HandleFunc("POST /api/join/{code}", a.handleJoin)

	// Mini App (user) endpoints.
	mux.HandleFunc("GET /api/me", a.auth(a.handleMe))
	mux.HandleFunc("GET /api/me/referrals", a.auth(a.handleMeReferrals))
	mux.HandleFunc("POST /api/me/devices", a.auth(a.handleCreateDevice))
	mux.HandleFunc("DELETE /api/me/devices/{id}", a.auth(a.handleDeleteDevice))
	mux.HandleFunc("GET /api/me/devices/{id}/config", a.auth(a.handleDeviceConfig))
	mux.HandleFunc("GET /api/me/devices/{id}/qr.png", a.auth(a.handleDeviceQR))
	mux.HandleFunc("POST /api/me/devices/{id}/join", a.auth(a.handleMeJoinCode))

	// App access key management (server login; see access_key.go). The
	// plaintext key leaves the server only from the create/rotate handler.
	mux.HandleFunc("POST /api/me/access-key", a.auth(a.handleMeAccessKeyCreate))
	mux.HandleFunc("DELETE /api/me/access-key", a.auth(a.handleMeAccessKeyDelete))

	// Admin endpoints (admin token or admin-role user).
	mux.HandleFunc("GET /api/admin/overview", a.admin(a.handleAdminOverview))
	mux.HandleFunc("GET /api/admin/users", a.admin(a.handleAdminListUsers))
	mux.HandleFunc("POST /api/admin/users", a.admin(a.handleAdminCreateUser))
	mux.HandleFunc("POST /api/admin/users/{id}/devices", a.admin(a.handleAdminCreateDevice))
	mux.HandleFunc("PATCH /api/admin/users/{id}", a.admin(a.handleAdminUpdateUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", a.admin(a.handleAdminDeleteUser))
	mux.HandleFunc("GET /api/admin/devices", a.admin(a.handleAdminDevices))
	mux.HandleFunc("GET /api/admin/devices/{id}/config", a.admin(a.handleAdminDeviceConfig))
	mux.HandleFunc("GET /api/admin/devices/{id}/qr.png", a.admin(a.handleAdminDeviceQR))
	mux.HandleFunc("PATCH /api/admin/devices/{id}", a.admin(a.handleAdminUpdateDevice))
	mux.HandleFunc("DELETE /api/admin/devices/{id}", a.admin(a.handleAdminDeleteDevice))

	// Admin invite endpoints (self-enrollment links; see docs/self-enroll.md).
	mux.HandleFunc("GET /api/admin/invites", a.admin(a.handleAdminListInvites))
	mux.HandleFunc("POST /api/admin/invites", a.admin(a.handleAdminCreateInvite))
	mux.HandleFunc("DELETE /api/admin/invites/{code}", a.admin(a.handleAdminDeleteInvite))

	// Quick-connect for admins: mint a join code for a device in the admin's
	// subtree (a reseller handing a config to their customer).
	mux.HandleFunc("POST /api/admin/devices/{id}/join", a.admin(a.handleAdminJoinCode))

	// Referral program endpoints (see docs/referrals.md). The money-facing
	// side (payments, payouts, settings) is the installation owner's alone.
	mux.HandleFunc("POST /api/admin/users/{id}/payments", a.admin(a.handleAdminCreatePayment))
	mux.HandleFunc("GET /api/admin/payments", a.admin(a.handleAdminListPayments))
	mux.HandleFunc("GET /api/admin/referrals", a.admin(a.handleAdminReferralOverview))
	mux.HandleFunc("POST /api/admin/referrals/payouts", a.admin(a.handleAdminCreatePayout))
	mux.HandleFunc("GET /api/admin/settings", a.admin(a.handleAdminGetSettings))
	mux.HandleFunc("PATCH /api/admin/settings", a.admin(a.handleAdminPatchSettings))

	// Admin node endpoints (multi-node registry; superadmin only — infra is
	// the installation owner's; see docs/multinode.md).
	mux.HandleFunc("GET /api/admin/nodes", a.admin(a.handleAdminListNodes))
	mux.HandleFunc("POST /api/admin/nodes", a.admin(a.handleAdminCreateNode))
	mux.HandleFunc("PATCH /api/admin/nodes/{id}", a.admin(a.handleAdminUpdateNode))
	mux.HandleFunc("DELETE /api/admin/nodes/{id}", a.admin(a.handleAdminDeleteNode))
	mux.HandleFunc("POST /api/admin/nodes/{id}/activate", a.admin(a.handleAdminActivateNode))
	mux.HandleFunc("POST /api/admin/nodes/{id}/token", a.admin(a.handleAdminRotateNodeToken))

	// One-click updates via the opt-in updater sidecar (see docs/updates.md).
	mux.HandleFunc("GET /api/admin/updates", a.admin(a.handleAdminUpdates))
	mux.HandleFunc("POST /api/admin/updates/apply", a.admin(a.handleAdminUpdatesApply))

	// Node agent endpoints (remote fibr-node binaries; bearer node token).
	mux.HandleFunc("POST /api/node/register", a.nodeAuth(a.handleNodeRegister))
	mux.HandleFunc("POST /api/node/sync", a.nodeAuth(a.handleNodeSync))

	return a.recoverMiddleware(mux)
}

/* ------------------------- auth plumbing ------------------------- */

// identity is the resolved caller. The static admin token maps to the
// superadmin role with userID 0; session tokens carry the user's own role
// ("user", "admin" or "superadmin" — see docs/roles.md).
type identity struct {
	userID int64  // 0 when identified by the admin token
	role   string // store.RoleUser | store.RoleAdmin | store.RoleSuperadmin
}

// isAdmin reports whether the caller may use the /api/admin/* surface at all
// (admins and superadmins; scope differences are enforced per handler).
func (id identity) isAdmin() bool {
	return id.role == store.RoleAdmin || id.role == store.RoleSuperadmin
}

// isSuper reports whether the caller is the installation owner: the static
// admin token or a user with role superadmin. Superadmins see and manage
// every account; admins only their own subtree.
func (id identity) isSuper() bool { return id.role == store.RoleSuperadmin }

type ctxKeyType struct{}

var ctxKey ctxKeyType

// msgSubscriptionExpired is the 403 reason for accounts past their effective
// deadline; the Mini App matches on it to show a localized "subscription
// expired" screen (see docs/billing.md).
const msgSubscriptionExpired = "subscription expired"

// identify resolves the caller from the Authorization header: the static
// admin token, a Mini App session token, or an app access key (server login
// for the Flutter client — resolved by SHA-256 hash comparison, never by
// storing the key). An account past its effective subscription deadline is
// rejected with the msgSubscriptionExpired reason (surfaces as 403), like a
// disabled one.
func (a *API) identify(r *http.Request) (identity, string, bool) {
	token := bearer(r)
	if token == "" {
		return identity{}, "", false
	}
	if token == a.cfg.AdminToken {
		return identity{role: store.RoleSuperadmin}, "", true
	}
	uid, ok := a.parseSessionToken(token)
	if !ok {
		// Not an admin token and not a session token — the last chance is an
		// app access key (a key carries its owner's rights; no separate role).
		u, err := a.store.GetUserByAccessTokenHash(hashAccessKey(token))
		if err != nil || u.Disabled {
			return identity{}, "", false
		}
		if a.userExpired(u) {
			return identity{}, msgSubscriptionExpired, false
		}
		return identity{userID: u.ID, role: u.Role}, "", true
	}
	u, err := a.store.GetUser(uid)
	if err != nil || u.Disabled {
		return identity{}, "", false
	}
	if a.userExpired(u) {
		return identity{}, msgSubscriptionExpired, false
	}
	return identity{userID: u.ID, role: u.Role}, "", true
}

// userExpired reports whether the account's effective deadline (its own
// expires_at OR its owner-admin's, whichever is earlier) has passed.
func (a *API) userExpired(u *store.User) bool {
	var ownerExp *time.Time
	if u.OwnerID != nil {
		if owner, err := a.store.GetUser(*u.OwnerID); err == nil {
			ownerExp = owner.ExpiresAt
		}
	}
	return store.ExpiredAt(store.EffectiveExpiry(u.ExpiresAt, ownerExp), time.Now())
}

// userExpiredByMap is userExpired over a preloaded id -> user map (list
// handlers already hold every account in memory; the owner of a scoped user
// is not in the scoped slice, so the map must be built from the full list).
func userExpiredByMap(byID map[int64]*store.User, u store.User, now time.Time) bool {
	var ownerExp *time.Time
	if u.OwnerID != nil {
		if owner := byID[*u.OwnerID]; owner != nil {
			ownerExp = owner.ExpiresAt
		}
	}
	return store.ExpiredAt(store.EffectiveExpiry(u.ExpiresAt, ownerExp), now)
}

// effectiveMaxDevices resolves the device cap that governs an account: its
// own max_devices and — for plain users — the owner-admin's, with the global
// FIBR_MAX_DEVICES as the fallback (store.EffectiveMaxDevices). Admins and
// superadmin users carry their own-or-default cap: their owners can only be
// superadmins (unlimited), so no cascade applies. The static admin token
// never creates devices for itself and is never the target here.
func (a *API) effectiveMaxDevices(u *store.User) (int, error) {
	var ownerMax *int64
	if u.OwnerID != nil && u.Role == store.RoleUser {
		owner, err := a.store.GetUser(*u.OwnerID)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return 0, err
			}
			// owner deleted (defensive; owner_id is ON DELETE SET NULL): no cap
		} else {
			ownerMax = owner.MaxDevices
		}
	}
	return store.EffectiveMaxDevices(u.MaxDevices, ownerMax, a.cfg.MaxDevices), nil
}

func (a *API) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, reason, ok := a.identify(r)
		if !ok || id.userID == 0 {
			code, msg := http.StatusUnauthorized, "unauthorized"
			if reason == msgSubscriptionExpired {
				code, msg = http.StatusForbidden, reason
			}
			a.errJSON(w, code, msg)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey, id)))
	}
}

func (a *API) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, reason, ok := a.identify(r)
		if !ok {
			if reason == msgSubscriptionExpired {
				a.errJSON(w, http.StatusForbidden, reason)
				return
			}
			a.errJSON(w, http.StatusForbidden, "admin only")
			return
		}
		if !id.isAdmin() {
			a.errJSON(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey, id)))
	}
}

func ident(r *http.Request) identity {
	id, _ := r.Context().Value(ctxKey).(identity)
	return id
}

/* ------------------------- public endpoints ------------------------- */

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// handlePublicInfo serves the unauthenticated bits the frontends need to
// render the login screen: the bot @username for the Telegram Login Widget
// (web cabinet), the invite-only mode flag and whether the xray protocol
// is enabled. No secrets here — the bot username is public by definition.
// The owner's contact line rides along so the "subscription expired" screen
// can tell the user who to pay without authenticating first.
func (a *API) handlePublicInfo(w http.ResponseWriter, r *http.Request) {
	botName := a.botUsername.Load().(string)
	contact, _ := a.store.GetSetting(store.SettingContactInfo)
	writeJSON(w, http.StatusOK, map[string]any{
		"bot_username":    botName,
		"domain":          a.cfg.Domain,
		"invite_required": a.cfg.RequireInvite,
		"xray_enabled":    a.cfg.XrayEnabled,
		"contact_info":    contact,
	})
}

func (a *API) handleAuthTelegram(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData     string `json:"init_data"`
		InviteCode   string `json:"invite_code"`   // required for NEW users when FIBR_REQUIRE_INVITE=true
		ReferralCode string `json:"referral_code"` // optional: whose referral link the user followed
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	tu, err := ValidateInitData(body.InitData, a.cfg.TelegramBotToken)
	if errors.Is(err, errBadInitData) {
		// The web cabinet (/cabinet/) sends Telegram Login Widget data to
		// this same endpoint — flat user fields, SHA256(bot_token) secret.
		tu, err = ValidateLoginWidget(body.InitData, a.cfg.TelegramBotToken)
	}
	if err != nil {
		a.errJSON(w, http.StatusUnauthorized, err.Error())
		return
	}

	u, err := a.store.GetUserByTelegramID(tu.ID)
	switch {
	case err == nil:
		// Refresh the stored username; upgrade to superadmin if listed in env
		// (covers accounts promoted to plain admin before the three-role
		// model existed).
		name := displayName(tu)
		if name != "" && name != u.Username {
			_ = a.store.SetUsername(u.ID, name)
			u.Username = name
		}
		if u.Role != store.RoleSuperadmin && containsInt64(a.cfg.AdminTelegramIDs, tu.ID) {
			_ = a.store.SetUserRole(u.ID, store.RoleSuperadmin)
			u.Role = store.RoleSuperadmin
		}
	case errors.Is(err, store.ErrNotFound):
		// Referral attribution: a valid code from a live account records who
		// invited the newcomer. An unknown or disabled referrer is ignored —
		// a mistyped link must not block registration.
		refCode := strings.TrimSpace(body.ReferralCode)
		var referrer *store.User
		if refCode != "" {
			if ref, err := a.store.GetUserByReferralCode(refCode); err == nil && !ref.Disabled && !a.userExpired(ref) {
				referrer = ref
			} else if err != nil && !errors.Is(err, store.ErrNotFound) {
				a.errJSON(w, http.StatusInternalServerError, "storage error")
				return
			}
		}
		// Self-referral cannot happen (the account does not exist yet), but a
		// telegram_id collision with the referrer would make one user their
		// own beneficiary; skip attribution in that case.
		if referrer != nil && referrer.TelegramID != nil && *referrer.TelegramID == tu.ID {
			referrer = nil
		}
		// Self-enroll gate: in invite-only mode a brand-new user must present
		// a valid unused code (existing users sign in freely). A valid
		// referral code counts as the invitation — that is the whole point of
		// sharing a referral link in invite-only mode.
		inviteCode := ""
		if a.cfg.RequireInvite && referrer == nil {
			inviteCode = strings.TrimSpace(body.InviteCode)
			if inviteCode == "" {
				a.errJSON(w, http.StatusForbidden, "invite required")
				return
			}
			if _, err := a.store.GetInvite(inviteCode); err != nil {
				a.errJSON(w, http.StatusForbidden, "invalid invite")
				return
			}
		}
		role := store.RoleUser
		if containsInt64(a.cfg.AdminTelegramIDs, tu.ID) {
			role = store.RoleSuperadmin
		}
		tid := tu.ID
		u = &store.User{TelegramID: &tid, Username: displayName(tu), Role: role}
		if referrer != nil {
			rid := referrer.ID
			u.ReferredBy = &rid
		}
		if err := a.store.CreateUser(u); err != nil {
			a.errJSON(w, http.StatusInternalServerError, "cannot create user")
			return
		}
		if inviteCode != "" {
			// Claim the code only after the account exists; if a concurrent
			// registration won the race, roll the account back — the code is
			// one-time and cannot be shared.
			if err := a.store.MarkUsed(inviteCode, u.ID); err != nil {
				_ = a.store.DeleteUser(u.ID)
				a.log.Warn("invite claim failed, registration rolled back", "invite", inviteCode, "user_id", u.ID, "error", err)
				a.errJSON(w, http.StatusForbidden, "invalid invite")
				return
			}
		}
		if referrer != nil {
			a.log.Info("user registered via mini app", "user_id", u.ID, "telegram_id", tu.ID, "invite", inviteCode, "referred_by", referrer.ID)
		} else {
			a.log.Info("user registered via mini app", "user_id", u.ID, "telegram_id", tu.ID, "invite", inviteCode)
		}
	default:
		a.errJSON(w, http.StatusInternalServerError, "storage error")
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

/* ------------------------- mini app endpoints ------------------------- */

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	u, err := a.store.GetUser(id.userID)
	if err != nil {
		a.errJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	devices, err := a.store.ListDevices(u.ID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	// The device cap is per-account with an owner cascade (like expires_at);
	// admins and superadmin users resolve to their own-or-default cap.
	limit, err := a.effectiveMaxDevices(u)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	out := make([]deviceOut, 0, len(devices))
	for _, d := range devices {
		out = append(out, newDeviceOut(d, u.Username))
	}
	ep, _, err := a.resolveActiveEndpoint()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	// Whether the account currently holds an app access key (the Mini App
	// profile shows "created" without ever learning the key or its hash).
	hasKey, err := a.store.HasUserAccessToken(u.ID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": u, "devices": out,
		"max_devices": limit, "xray_enabled": a.cfg.XrayEnabled,
		"has_access_key": hasKey,
		// The client compares endpoint_changed_at with its stored copy to
		// decide a re-download is needed (multi-node endpoint switch).
		"active_node":         ep.Name,
		"endpoint_changed_at": a.endpointChangedAt(),
	})
}

func (a *API) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	existing, err := a.store.ListDevices(id.userID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	u, err := a.store.GetUser(id.userID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	limit, err := a.effectiveMaxDevices(u)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	if len(existing) >= limit {
		a.errJSON(w, http.StatusConflict, "device limit reached")
		return
	}

	var body struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"` // "wg" (default) | "xray"
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	protocol, err := normalizeProtocol(body.Protocol, a.cfg.XrayEnabled)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := a.provisionDevice(id.userID, sanitizeName(body.Name, fmt.Sprintf("device-%d", len(existing)+1)), protocol)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, newDeviceOut(*d, ""))
}

func (a *API) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	d, ok := a.ownedDevice(w, r, id.userID)
	if !ok {
		return
	}
	if err := a.store.DeleteDevice(d.ID); err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot delete device")
		return
	}
	a.syncPeers()
	a.syncXray()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleDeviceConfig(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	d, ok := a.ownedDevice(w, r, id.userID)
	if !ok {
		return
	}
	a.serveConfig(w, *d)
}

func (a *API) handleDeviceQR(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	d, ok := a.ownedDevice(w, r, id.userID)
	if !ok {
		return
	}
	a.serveQR(w, *d)
}

/* ------------------------- admin endpoints ------------------------- */

// Overview is the aggregate snapshot used by the admin dashboard and /stats.
type Overview struct {
	Server struct {
		PublicKey   string   `json:"public_key"`
		Port        int      `json:"port"`
		Host        string   `json:"host"`
		Subnet      string   `json:"subnet"`
		DNS         []string `json:"dns"`
		MTU         int      `json:"mtu"`
		Passive     bool     `json:"passive"`
		XrayEnabled bool     `json:"xray_enabled"`
	} `json:"server"`
	Totals struct {
		Users   int   `json:"users"`
		Devices int   `json:"devices"`
		Online  int   `json:"online"`
		Expired int   `json:"expired"` // accounts past their effective deadline
		TotalRx int64 `json:"total_rx"`
		TotalTx int64 `json:"total_tx"`
	} `json:"totals"`
	Throughput struct {
		WgInBps   float64 `json:"wg_in_bps"`
		WgOutBps  float64 `json:"wg_out_bps"`
		ExtInBps  float64 `json:"ext_in_bps"`
		ExtOutBps float64 `json:"ext_out_bps"`
	} `json:"throughput"`
	RecentDevices []deviceOut `json:"recent_devices"`
	// Multi-node: which node's endpoint client configs currently render
	// against, and when that last changed (unix seconds; 0 = never).
	ActiveNode        string `json:"active_node"`
	EndpointChangedAt int64  `json:"endpoint_changed_at"`
}

// BuildOverview computes the current stats snapshot.
func (a *API) BuildOverview() (*Overview, error) {
	ov := &Overview{}
	ep, _, err := a.resolveActiveEndpoint()
	if err != nil {
		return nil, err
	}
	ov.Server.PublicKey = ep.WGServerPubKey
	ov.Server.Port = ep.WGPort
	ov.Server.Host = ep.Host
	ov.Server.Subnet = a.cfg.Subnet
	ov.Server.DNS = a.cfg.DNS
	ov.Server.MTU = a.cfg.MTU
	ov.Server.Passive = a.wg.Passive()
	ov.Server.XrayEnabled = a.cfg.XrayEnabled
	ov.ActiveNode = ep.Name
	ov.EndpointChangedAt = a.endpointChangedAt()

	users, err := a.store.ListUsers()
	if err != nil {
		return nil, err
	}
	devices, err := a.store.ListDevices(0)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(users))
	byID := make(map[int64]*store.User, len(users))
	for i := range users {
		names[users[i].ID] = users[i].Username
		byID[users[i].ID] = &users[i]
	}
	ov.Totals.Users = len(users)
	ov.Totals.Devices = len(devices)
	now := time.Now()
	for _, u := range users {
		if userExpiredByMap(byID, u, now) {
			ov.Totals.Expired++
		}
	}
	for _, d := range devices {
		ov.Totals.TotalRx += d.TotalRx
		ov.Totals.TotalTx += d.TotalTx
		if isOnline(d) {
			ov.Totals.Online++
		}
	}
	ov.Throughput.WgInBps, ov.Throughput.WgOutBps, ov.Throughput.ExtInBps, ov.Throughput.ExtOutBps = a.wg.Throughput()

	// Most recently active devices first.
	sorted := make([]store.Device, len(devices))
	copy(sorted, devices)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].LastHandshake.After(sorted[i].LastHandshake) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	if len(sorted) > 10 {
		sorted = sorted[:10]
	}
	for _, d := range sorted {
		ov.RecentDevices = append(ov.RecentDevices, newDeviceOut(d, names[d.UserID]))
	}
	return ov, nil
}

func (a *API) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	// Installation-wide metrics are the owner's alone: a reseller must not
	// learn how big the whole platform is.
	if !ident(r).isSuper() {
		a.errJSON(w, http.StatusForbidden, "superadmin only")
		return
	}
	ov, err := a.BuildOverview()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "cannot build overview")
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

type userOut struct {
	store.User
	Devices int   `json:"devices"`
	Online  int   `json:"online"`
	TotalRx int64 `json:"total_rx"`
	TotalTx int64 `json:"total_tx"`
	// Current transfer rates (bit/s): the sum of the user's devices' latest
	// sample deltas — "what the port is doing right now for this account".
	SpeedRxBps float64 `json:"speed_rx_bps"`
	SpeedTxBps float64 `json:"speed_tx_bps"`
	// Expired is the effective-deadline verdict (own expires_at OR the
	// owner-admin's, whichever is earlier) so the frontends never have to
	// resolve the cascade themselves.
	Expired bool `json:"expired"`
	// ReferredByName is whose referral link the account signed up with
	//("" = organic) — the superadmin's attribution column.
	ReferredByName string `json:"referred_by_name"`
}

func (a *API) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	users, err := a.store.ListUsers()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	// Effective-expiry needs the owner's deadline too, and an owner may fall
	// outside the scoped slice below (an admin is not their own subtree) —
	// so resolve it against the full list before scoping.
	byID := make(map[int64]*store.User, len(users))
	for i := range users {
		byID[users[i].ID] = &users[i]
	}
	now := time.Now()
	// Scope: the superadmin sees every account; an admin sees only the users
	// they created (owner_id = their id). The admin themself is NOT included —
	// their own devices live in /api/me.
	if !id.isSuper() {
		scoped := make([]store.User, 0, len(users))
		for _, u := range users {
			if u.OwnerID != nil && *u.OwnerID == id.userID {
				scoped = append(scoped, u)
			}
		}
		users = scoped
	}
	devices, err := a.store.ListDevices(0)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	stats := make(map[int64]*userOut, len(users))
	out := make([]userOut, 0, len(users))
	for _, u := range users {
		s := userOut{User: u, Expired: userExpiredByMap(byID, u, now)}
		if u.ReferredBy != nil {
			if ref := byID[*u.ReferredBy]; ref != nil {
				s.ReferredByName = ref.Username
			}
		}
		stats[u.ID] = &s
		out = append(out, s)
	}
	// Current per-user speed: sum of their devices' latest rates.
	speeds := a.wg.DeviceSpeeds()
	for _, d := range devices {
		if s, ok := stats[d.UserID]; ok {
			s.Devices++
			s.TotalRx += d.TotalRx
			s.TotalTx += d.TotalTx
			if sp, okd := speeds[d.ID]; okd {
				s.SpeedRxBps += sp.RxBps
				s.SpeedTxBps += sp.TxBps
			}
			if isOnline(d) {
				s.Online++
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleAdminCreateUser(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	var body struct {
		TelegramID *int64 `json:"telegram_id"`
		Username   string `json:"username"`
		Role       string `json:"role"`
		ExpiresAt  string `json:"expires_at"` // RFC3339; only the superadmin's value is honored
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	role := body.Role
	owner := id.userID // 0 = the static token: no owner to record
	switch {
	case id.isSuper():
		// The superadmin may found resellers: user or admin, never another
		// superadmin by hand (that role belongs to the env/static token).
		if role != "" && role != store.RoleUser && role != store.RoleAdmin {
			a.errJSON(w, http.StatusBadRequest, "role must be user or admin")
			return
		}
	default:
		// An admin may only create plain users, always owned by themself.
		if role != "" && role != store.RoleUser {
			a.errJSON(w, http.StatusBadRequest, "role must be user or admin")
			return
		}
		role = store.RoleUser
		o := id.userID
		owner = o
	}
	u := &store.User{TelegramID: body.TelegramID, Username: sanitizeName(body.Username, ""), Role: role}
	if owner > 0 {
		u.OwnerID = &owner
	}
	// Selling access at creation time is the superadmin's move; an admin's
	// expires_at (if any) is silently ignored — deadlines are set by the
	// owner of the installation (see docs/billing.md).
	if id.isSuper() && body.ExpiresAt != "" {
		ts, err := time.Parse(time.RFC3339, body.ExpiresAt)
		if err != nil {
			a.errJSON(w, http.StatusBadRequest, "expires_at must be an RFC3339 timestamp")
			return
		}
		t := ts
		u.ExpiresAt = &t
	}
	if err := a.store.CreateUser(u); err != nil {
		a.errJSON(w, http.StatusConflict, "user already exists")
		return
	}
	a.log.Info("user created", "user_id", u.ID, "role", u.Role, "owner_id", owner, "by", id.userID)
	writeJSON(w, http.StatusCreated, u)
}

// fetchManagedUser loads the target user and enforces the reseller scope.
// Foreign (or own) accounts are reported as 404 so an admin cannot even
// probe which ids exist; role changes are the superadmin's alone.
func (a *API) fetchManagedUser(w http.ResponseWriter, r *http.Request) (*store.User, identity, bool) {
	caller := ident(r)
	id, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return nil, caller, false
	}
	u, err := a.store.GetUser(id)
	if err != nil {
		a.errJSON(w, http.StatusNotFound, "user not found")
		return nil, caller, false
	}
	if !caller.isSuper() && (u.OwnerID == nil || *u.OwnerID != caller.userID) {
		a.errJSON(w, http.StatusNotFound, "user not found")
		return nil, caller, false
	}
	return u, caller, true
}

// patchExpiry decodes the optional expires_at PATCH field, which is
// tri-state: absent (leave the deadline alone), JSON null (clear = unlimited)
// or an RFC3339 timestamp string. set tells whether the deadline should be
// written at all; unix is the value to write (nil = NULL).
func patchExpiry(raw json.RawMessage) (set bool, unix *int64, err error) {
	if len(raw) == 0 {
		return false, nil, nil // field absent: no change
	}
	if string(raw) == "null" {
		return true, nil, nil // explicit null: unlimited
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return false, nil, errors.New("expires_at must be an RFC3339 timestamp or null")
	}
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return false, nil, errors.New("expires_at must be an RFC3339 timestamp or null")
	}
	v := ts.Unix()
	return true, &v, nil
}

// patchMaxDevices decodes the optional max_devices PATCH field, tri-state
// like expires_at: absent (leave the cap alone), JSON null (reset to the
// global default) or a non-negative integer (0 = "no new devices").
func patchMaxDevices(raw json.RawMessage) (set bool, n *int64, err error) {
	if len(raw) == 0 {
		return false, nil, nil // field absent: no change
	}
	if string(raw) == "null" {
		return true, nil, nil // explicit null: global default
	}
	var v int64
	if err := json.Unmarshal(raw, &v); err != nil || v < 0 {
		return false, nil, errors.New("max_devices must be a non-negative integer or null")
	}
	return true, &v, nil
}

func (a *API) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	u, caller, ok := a.fetchManagedUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Role       *string         `json:"role"`
		Disabled   *bool           `json:"disabled"`
		ExpiresAt  json.RawMessage `json:"expires_at"`  // RFC3339 | null; superadmin only
		MaxDevices json.RawMessage `json:"max_devices"` // int >= 0 | null; superadmin any, admin capped
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Role != nil {
		if !caller.isSuper() {
			a.errJSON(w, http.StatusForbidden, "superadmin only")
			return
		}
		if *body.Role != store.RoleUser && *body.Role != store.RoleAdmin && *body.Role != store.RoleSuperadmin {
			a.errJSON(w, http.StatusBadRequest, "role must be user, admin or superadmin")
			return
		}
		if err := a.store.SetUserRole(u.ID, *body.Role); err != nil {
			a.errJSON(w, http.StatusNotFound, "user not found")
			return
		}
	}
	if body.Disabled != nil {
		if err := a.store.SetUserDisabled(u.ID, *body.Disabled); err != nil {
			a.errJSON(w, http.StatusNotFound, "user not found")
			return
		}
	}
	if len(body.ExpiresAt) > 0 {
		// Selling time is the installation owner's alone: a reseller must not
		// extend their own or their users' deadline for free.
		if !caller.isSuper() {
			a.errJSON(w, http.StatusForbidden, "superadmin only")
			return
		}
		set, unix, err := patchExpiry(body.ExpiresAt)
		if err != nil {
			a.errJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		if set {
			if err := a.store.SetUserExpiry(u.ID, unix); err != nil {
				a.errJSON(w, http.StatusNotFound, "user not found")
				return
			}
			// Apply it to the tunnel right away instead of waiting for the
			// periodic expiry pass (a past timestamp cuts the peers now, a
			// renewal or null restores them).
			a.syncPeers()
			a.syncXray()
			a.log.Info("user expiry set", "user_id", u.ID, "expires_at", unix)
		}
	}
	if len(body.MaxDevices) > 0 {
		set, n, err := patchMaxDevices(body.MaxDevices)
		if err != nil {
			a.errJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		if set {
			// A reseller may cap their own users, but never above their own
			// effective cap (they cannot sell device slots they do not have).
			// The static token / superadmin sessions are uncapped. A null
			// reset is always safe: the owner cascade still bounds the user.
			if !caller.isSuper() {
				callerUser, err := a.store.GetUser(caller.userID)
				if err != nil {
					a.errJSON(w, http.StatusForbidden, "device limit exceeds your own")
					return
				}
				cap, err := a.effectiveMaxDevices(callerUser)
				if err != nil {
					a.errJSON(w, http.StatusInternalServerError, "storage error")
					return
				}
				if n != nil && int(*n) > cap {
					a.errJSON(w, http.StatusForbidden, "device limit exceeds your own")
					return
				}
			}
			if err := a.store.SetUserMaxDevices(u.ID, n); err != nil {
				a.errJSON(w, http.StatusNotFound, "user not found")
				return
			}
			// No tunnel change: the cap only gates future device creation.
			a.log.Info("user max_devices set", "user_id", u.ID, "max_devices", n)
		}
	}
	u, err := a.store.GetUser(u.ID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *API) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	u, _, ok := a.fetchManagedUser(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteUser(u.ID); err != nil {
		a.errJSON(w, http.StatusNotFound, "user not found")
		return
	}
	a.syncPeers() // their devices cascade-deleted, peers must go too
	a.syncXray()
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminCreateDevice provisions a device for any user (works for manual
// panel-created users who cannot use the Mini App login). Admins may only
// issue devices to their own users.
func (a *API) handleAdminCreateDevice(w http.ResponseWriter, r *http.Request) {
	u, _, ok := a.fetchManagedUser(w, r)
	if !ok {
		return
	}
	uid := u.ID
	existing, err := a.store.ListDevices(uid)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	limit, err := a.effectiveMaxDevices(u)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	if len(existing) >= limit {
		a.errJSON(w, http.StatusConflict, "device limit reached")
		return
	}
	var body struct {
		Name     string `json:"name"`
		Protocol string `json:"protocol"` // "wg" (default) | "xray"
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	protocol, err := normalizeProtocol(body.Protocol, a.cfg.XrayEnabled)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := a.provisionDevice(uid, sanitizeName(body.Name, fmt.Sprintf("device-%d", len(existing)+1)), protocol)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, newDeviceOut(*d, ""))
}

// normalizeProtocol validates the requested device protocol: "" means the
// default ("wg"), anything but "wg"/"xray" is rejected, and "xray" requires
// xray to be enabled in the server config.
func normalizeProtocol(p string, xrayEnabled bool) (string, error) {
	switch p = strings.TrimSpace(p); p {
	case "":
		return store.ProtocolWG, nil
	case store.ProtocolWG:
		return p, nil
	case store.ProtocolXray:
		if !xrayEnabled {
			return "", fmt.Errorf("xray protocol is not enabled on this server (FIBR_XRAY_ENABLED)")
		}
		return p, nil
	default:
		return "", fmt.Errorf("protocol must be wg or xray, got %q", p)
	}
}

// provisionDevice generates credentials and stores the device.
//
// wg:    Curve25519 keypair + PSK; keys in the usual fields.
// xray:  random UUID in PublicKey, PrivateKey/PresharedKey stay empty
//
//	(documented in store.Device). An IPv4 is still allocated to satisfy
//	the UNIQUE column but is unused by the protocol.
func (a *API) provisionDevice(userID int64, name, protocol string) (*store.Device, error) {
	ip, err := a.store.NextIPv4(a.cfg.Subnet)
	if err != nil {
		return nil, fmt.Errorf("no free addresses in subnet")
	}
	d := &store.Device{
		UserID:  userID,
		Name:    name,
		IPv4:    ip,
		Enabled: true,
	}
	switch protocol {
	case store.ProtocolXray:
		d.Protocol = store.ProtocolXray
		d.PublicKey = xray.NewUUID() // vless client id; see store.Device
	default:
		priv, err := wgtypes.GeneratePrivateKey()
		if err != nil {
			return nil, fmt.Errorf("key generation failed")
		}
		psk, err := wg.GeneratePresharedKey()
		if err != nil {
			return nil, fmt.Errorf("key generation failed")
		}
		d.Protocol = store.ProtocolWG
		d.PublicKey = priv.PublicKey().String()
		d.PrivateKey = priv.String()
		d.PresharedKey = psk.String()
	}
	if err := a.store.CreateDevice(d); err != nil {
		return nil, fmt.Errorf("cannot create device")
	}
	a.syncPeers()
	a.syncXray()
	return d, nil
}

func (a *API) handleAdminDevices(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	users, err := a.store.ListUsers()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	devices, err := a.store.ListDevices(0)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	// Reseller scope: an admin only sees devices of their subtree (themself
	// and the users they created); the superadmin sees everything.
	var visible map[int64]bool
	if !id.isSuper() {
		ids, err := a.visibleUserIDs(id)
		if err != nil {
			a.errJSON(w, http.StatusInternalServerError, "storage error")
			return
		}
		visible = make(map[int64]bool, len(ids))
		for _, uid := range ids {
			visible[uid] = true
		}
	}
	names := make(map[int64]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Username
	}
	speeds := a.wg.DeviceSpeeds()
	out := make([]deviceOut, 0, len(devices))
	for _, d := range devices {
		if visible != nil && !visible[d.UserID] {
			continue
		}
		o := newDeviceOut(d, names[d.UserID])
		if sp, ok := speeds[d.ID]; ok {
			o.SpeedRxBps, o.SpeedTxBps = sp.RxBps, sp.TxBps
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleAdminDeviceConfig(w http.ResponseWriter, r *http.Request) {
	d, ok := a.scopedDevice(w, r)
	if !ok {
		return
	}
	a.serveConfig(w, *d)
}

func (a *API) handleAdminDeviceQR(w http.ResponseWriter, r *http.Request) {
	d, ok := a.scopedDevice(w, r)
	if !ok {
		return
	}
	a.serveQR(w, *d)
}

func (a *API) handleAdminUpdateDevice(w http.ResponseWriter, r *http.Request) {
	d, ok := a.scopedDevice(w, r)
	if !ok {
		return
	}
	id := d.ID
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Enabled != nil {
		if err := a.store.SetDeviceEnabled(id, *body.Enabled); err != nil {
			a.errJSON(w, http.StatusNotFound, "device not found")
			return
		}
		a.syncPeers()
		a.syncXray()
	}
	d, err := a.store.GetDevice(id)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, newDeviceOut(*d, ""))
}

func (a *API) handleAdminDeleteDevice(w http.ResponseWriter, r *http.Request) {
	d, ok := a.scopedDevice(w, r)
	if !ok {
		return
	}
	if err := a.store.DeleteDevice(d.ID); err != nil {
		a.errJSON(w, http.StatusNotFound, "device not found")
		return
	}
	a.syncPeers()
	a.syncXray()
	w.WriteHeader(http.StatusNoContent)
}

/* ------------------------- shared helpers ------------------------- */

// deviceOut is the JSON shape of a device (private keys never leave the
// server except inside rendered configs).
type deviceOut struct {
	ID              int64      `json:"id"`
	UserID          int64      `json:"user_id"`
	Username        string     `json:"username,omitempty"`
	Name            string     `json:"name"`
	Protocol        string     `json:"protocol"` // "wg" | "xray"
	PublicKey       string     `json:"public_key"`
	IPv4            string     `json:"ipv4"`
	Enabled         bool       `json:"enabled"`
	Online          bool       `json:"online"`
	CreatedAt       time.Time  `json:"created_at"`
	LastHandshake   *time.Time `json:"last_handshake"`
	TotalRx         int64      `json:"total_rx"`
	TotalTx         int64      `json:"total_tx"`
	ConfigFetchedAt *time.Time `json:"config_fetched_at"` // multi-node staleness signal (nil = never)
	// Current transfer rates (bit/s) from the latest stats sample; zero for
	// xray devices (no counters) and until the second sample.
	SpeedRxBps float64 `json:"speed_rx_bps"`
	SpeedTxBps float64 `json:"speed_tx_bps"`
}

func newDeviceOut(d store.Device, username string) deviceOut {
	out := deviceOut{
		ID: d.ID, UserID: d.UserID, Username: username, Name: d.Name,
		Protocol: d.Protocol, PublicKey: d.PublicKey, IPv4: d.IPv4, Enabled: d.Enabled,
		Online: isOnline(d), CreatedAt: d.CreatedAt,
		TotalRx: d.TotalRx, TotalTx: d.TotalTx, ConfigFetchedAt: d.ConfigFetchedAt,
	}
	if out.Protocol == "" {
		out.Protocol = store.ProtocolWG
	}
	if !d.LastHandshake.IsZero() {
		t := d.LastHandshake
		out.LastHandshake = &t
	}
	return out
}

func isOnline(d store.Device) bool {
	return !d.LastHandshake.IsZero() && time.Since(d.LastHandshake) < 3*time.Minute
}

func (a *API) ownedDevice(w http.ResponseWriter, r *http.Request, userID int64) (*store.Device, bool) {
	id, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return nil, false
	}
	d, err := a.store.GetDevice(id)
	if err != nil || d.UserID != userID {
		a.errJSON(w, http.StatusNotFound, "device not found")
		return nil, false
	}
	return d, true
}

// scopedDevice loads a device for the admin config/QR/update/delete
// endpoints. The superadmin may touch any device; an admin only devices
// belonging to their subtree (themself or their users) — everything else
// reads as 404, same as a non-owner sees on /api/me.
func (a *API) scopedDevice(w http.ResponseWriter, r *http.Request) (*store.Device, bool) {
	id := ident(r)
	did, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return nil, false
	}
	d, err := a.store.GetDevice(did)
	if err != nil {
		a.errJSON(w, http.StatusNotFound, "device not found")
		return nil, false
	}
	if !id.isSuper() {
		ids, err := a.visibleUserIDs(id)
		if err != nil {
			a.errJSON(w, http.StatusInternalServerError, "storage error")
			return nil, false
		}
		if !containsInt64(ids, d.UserID) {
			a.errJSON(w, http.StatusNotFound, "device not found")
			return nil, false
		}
	}
	return d, true
}

// renderCredential renders the device's credential text against the ACTIVE
// node endpoint (multi-node: the active node's host/ports/keys go into the
// config instead of this box's own — see docs/multinode.md). The returned
// status code is 0 on success.
func (a *API) renderCredential(d store.Device) (content string, code int, err error) {
	ep, _, err := a.resolveActiveEndpoint()
	if err != nil {
		return "", http.StatusInternalServerError, errors.New("cannot resolve active endpoint")
	}
	if d.Protocol == store.ProtocolXray {
		if !a.cfg.XrayEnabled {
			return "", http.StatusConflict, errors.New("xray protocol is not enabled on this server")
		}
		return a.xray.ClientLinkAt(d, ep), 0, nil
	}
	return a.wg.ClientConfigAt(d, ep), 0, nil
}

// serveConfig renders the device credential: a wg-quick config for "wg"
// devices or the vless:// share link (text/plain) for "xray" devices.
func (a *API) serveConfig(w http.ResponseWriter, d store.Device) {
	content, code, err := a.renderCredential(d)
	if err != nil {
		a.errJSON(w, code, err.Error())
		return
	}
	suffix := "conf"
	if d.Protocol == store.ProtocolXray {
		suffix = "txt"
	}
	a.touchConfigFetched(d.ID)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="fibr-%s.%s"`, fileSafe(d.Name), suffix))
	w.Write([]byte(content))
}

// serveQR renders a PNG QR code of the device credential: the wg-quick config
// or the vless:// link.
func (a *API) serveQR(w http.ResponseWriter, d store.Device) {
	content, code, err := a.renderCredential(d)
	if err != nil {
		a.errJSON(w, code, err.Error())
		return
	}
	png, err := qrcode.Encode(content, qrcode.Medium, 512)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "qr generation failed")
		return
	}
	a.touchConfigFetched(d.ID)
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

// touchConfigFetched stamps the config delivery time; best-effort.
func (a *API) touchConfigFetched(deviceID int64) {
	if err := a.store.TouchDeviceConfigFetched(deviceID, time.Now()); err != nil {
		a.log.Warn("cannot stamp config_fetched_at", "device_id", deviceID, "error", err)
	}
}

// syncPeers pushes the current device set to WireGuard in the background and
// bumps the peer revision so remote node agents re-pull on their next tick.
// The bump is synchronous: it is a single settings upsert, and doing it
// inline guarantees the revision never lags a mutation (and never races the
// caller's teardown in tests).
func (a *API) syncPeers() {
	if _, err := a.store.BumpPeerRevision(); err != nil {
		a.log.Error("peer revision bump failed", "error", err)
	}
	go func() {
		if err := a.wg.SyncPeers(); err != nil {
			a.log.Error("peer sync after mutation failed", "error", err)
		}
	}()
}

// syncXray rewrites /data/xray/config.json in the background. The xray
// container still needs a manual restart to pick the file up; see the xray
// package doc.
func (a *API) syncXray() {
	if a.xray == nil {
		return // xray manager failed to start; xray is disabled
	}
	go func() {
		if err := a.xray.SyncClients(); err != nil {
			a.log.Error("xray config sync after mutation failed", "error", err)
		}
	}()
}

func (a *API) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				a.log.Error("panic in handler", "error", rec, "path", r.URL.Path)
				a.errJSON(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid json body")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) errJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func containsInt64(list []int64, v int64) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func displayName(u InitUser) string {
	if u.Username != "" {
		return u.Username
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	return strings.TrimPrefix(name, "@")
}

func sanitizeName(s, fallback string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if len(s) > 32 {
		s = s[:32]
	}
	if s == "" {
		return fallback
	}
	return s
}

func fileSafe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "device"
	}
	return b.String()
}
