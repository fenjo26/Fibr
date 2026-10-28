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
	"strconv"
	"strings"
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
	cfg   *config.Config
	store *store.Store
	wg    *wg.Manager
	xray  *xray.Manager
	log   *slog.Logger
}

// New creates the API.
func New(cfg *config.Config, st *store.Store, wgm *wg.Manager, xm *xray.Manager, log *slog.Logger) *API {
	return &API{cfg: cfg, store: st, wg: wgm, xray: xm, log: log}
}

// Handler builds the routed, panic-safe HTTP handler.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("POST /api/auth/telegram", a.handleAuthTelegram)

	// Mini App (user) endpoints.
	mux.HandleFunc("GET /api/me", a.auth(a.handleMe))
	mux.HandleFunc("POST /api/me/devices", a.auth(a.handleCreateDevice))
	mux.HandleFunc("DELETE /api/me/devices/{id}", a.auth(a.handleDeleteDevice))
	mux.HandleFunc("GET /api/me/devices/{id}/config", a.auth(a.handleDeviceConfig))
	mux.HandleFunc("GET /api/me/devices/{id}/qr.png", a.auth(a.handleDeviceQR))

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

// identify resolves the caller from the Authorization header: either the
// static admin token or a Mini App session token. An account past its
// effective subscription deadline is rejected with the
// msgSubscriptionExpired reason (surfaces as 403), like a disabled one.
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
		return identity{}, "", false
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

func (a *API) handleAuthTelegram(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData   string `json:"init_data"`
		InviteCode string `json:"invite_code"` // required for NEW users when FIBR_REQUIRE_INVITE=true
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	tu, err := ValidateInitData(body.InitData, a.cfg.TelegramBotToken)
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
		// Self-enroll gate: in invite-only mode a brand-new user must present
		// a valid unused code (existing users sign in freely).
		inviteCode := ""
		if a.cfg.RequireInvite {
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
		a.log.Info("user registered via mini app", "user_id", u.ID, "telegram_id", tu.ID, "invite", inviteCode)
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
	out := make([]deviceOut, 0, len(devices))
	for _, d := range devices {
		out = append(out, newDeviceOut(d, u.Username))
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "devices": out, "max_devices": a.cfg.MaxDevices, "xray_enabled": a.cfg.XrayEnabled})
}

func (a *API) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	existing, err := a.store.ListDevices(id.userID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	if len(existing) >= a.cfg.MaxDevices {
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
}

// BuildOverview computes the current stats snapshot.
func (a *API) BuildOverview() (*Overview, error) {
	ov := &Overview{}
	ov.Server.PublicKey = a.wg.PublicKey()
	ov.Server.Port = a.cfg.WGPort
	ov.Server.Host = a.cfg.WGHost
	ov.Server.Subnet = a.cfg.Subnet
	ov.Server.DNS = a.cfg.DNS
	ov.Server.MTU = a.cfg.MTU
	ov.Server.Passive = a.wg.Passive()
	ov.Server.XrayEnabled = a.cfg.XrayEnabled

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
	// Expired is the effective-deadline verdict (own expires_at OR the
	// owner-admin's, whichever is earlier) so the frontends never have to
	// resolve the cascade themselves.
	Expired bool `json:"expired"`
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
		stats[u.ID] = &s
		out = append(out, s)
	}
	for _, d := range devices {
		if s, ok := stats[d.UserID]; ok {
			s.Devices++
			s.TotalRx += d.TotalRx
			s.TotalTx += d.TotalTx
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

func (a *API) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	u, caller, ok := a.fetchManagedUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Role      *string         `json:"role"`
		Disabled  *bool           `json:"disabled"`
		ExpiresAt json.RawMessage `json:"expires_at"` // RFC3339 | null; superadmin only
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
	if len(existing) >= a.cfg.MaxDevices {
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
	out := make([]deviceOut, 0, len(devices))
	for _, d := range devices {
		if visible != nil && !visible[d.UserID] {
			continue
		}
		out = append(out, newDeviceOut(d, names[d.UserID]))
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
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	Username      string     `json:"username,omitempty"`
	Name          string     `json:"name"`
	Protocol      string     `json:"protocol"` // "wg" | "xray"
	PublicKey     string     `json:"public_key"`
	IPv4          string     `json:"ipv4"`
	Enabled       bool       `json:"enabled"`
	Online        bool       `json:"online"`
	CreatedAt     time.Time  `json:"created_at"`
	LastHandshake *time.Time `json:"last_handshake"`
	TotalRx       int64      `json:"total_rx"`
	TotalTx       int64      `json:"total_tx"`
}

func newDeviceOut(d store.Device, username string) deviceOut {
	out := deviceOut{
		ID: d.ID, UserID: d.UserID, Username: username, Name: d.Name,
		Protocol: d.Protocol, PublicKey: d.PublicKey, IPv4: d.IPv4, Enabled: d.Enabled,
		Online: isOnline(d), CreatedAt: d.CreatedAt,
		TotalRx: d.TotalRx, TotalTx: d.TotalTx,
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

// serveConfig renders the device credential: a wg-quick config for "wg"
// devices or the vless:// share link (text/plain) for "xray" devices.
func (a *API) serveConfig(w http.ResponseWriter, d store.Device) {
	if d.Protocol == store.ProtocolXray {
		if !a.cfg.XrayEnabled {
			a.errJSON(w, http.StatusConflict, "xray protocol is not enabled on this server")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="fibr-%s.txt"`, fileSafe(d.Name)))
		w.Write([]byte(a.xray.ClientLink(d)))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="fibr-%s.conf"`, fileSafe(d.Name)))
	w.Write([]byte(a.wg.ClientConfig(d)))
}

// serveQR renders a PNG QR code of the device credential: the wg-quick config
// or the vless:// link.
func (a *API) serveQR(w http.ResponseWriter, d store.Device) {
	content := a.wg.ClientConfig(d)
	if d.Protocol == store.ProtocolXray {
		if !a.cfg.XrayEnabled {
			a.errJSON(w, http.StatusConflict, "xray protocol is not enabled on this server")
			return
		}
		content = a.xray.ClientLink(d)
	}
	png, err := qrcode.Encode(content, qrcode.Medium, 512)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "qr generation failed")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

// syncPeers pushes the current device set to WireGuard in the background.
func (a *API) syncPeers() {
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
