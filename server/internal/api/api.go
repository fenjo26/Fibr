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
	"github.com/skip2/go-qrcode"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// API bundles the HTTP handlers.
type API struct {
	cfg   *config.Config
	store *store.Store
	wg    *wg.Manager
	log   *slog.Logger
}

// New creates the API.
func New(cfg *config.Config, st *store.Store, wgm *wg.Manager, log *slog.Logger) *API {
	return &API{cfg: cfg, store: st, wg: wgm, log: log}
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

	return a.recoverMiddleware(mux)
}

/* ------------------------- auth plumbing ------------------------- */

type identity struct {
	userID  int64 // 0 when identified by the admin token
	isAdmin bool
}

type ctxKeyType struct{}

var ctxKey ctxKeyType

// identify resolves the caller from the Authorization header: either the
// static admin token or a Mini App session token.
func (a *API) identify(r *http.Request) (identity, bool) {
	token := bearer(r)
	if token == "" {
		return identity{}, false
	}
	if token == a.cfg.AdminToken {
		return identity{isAdmin: true}, true
	}
	uid, ok := a.parseSessionToken(token)
	if !ok {
		return identity{}, false
	}
	u, err := a.store.GetUser(uid)
	if err != nil || u.Disabled {
		return identity{}, false
	}
	return identity{userID: u.ID, isAdmin: u.Role == "admin"}, true
}

func (a *API) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := a.identify(r)
		if !ok || id.userID == 0 {
			a.errJSON(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey, id)))
	}
}

func (a *API) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := a.identify(r)
		if !ok || !id.isAdmin {
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
		InitData string `json:"init_data"`
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
		// Refresh the stored username; upgrade to admin if listed in env.
		name := displayName(tu)
		if name != "" && name != u.Username {
			_ = a.store.SetUsername(u.ID, name)
			u.Username = name
		}
		if u.Role != "admin" && containsInt64(a.cfg.AdminTelegramIDs, tu.ID) {
			_ = a.store.SetUserRole(u.ID, "admin")
			u.Role = "admin"
		}
	case errors.Is(err, store.ErrNotFound):
		role := "user"
		if containsInt64(a.cfg.AdminTelegramIDs, tu.ID) {
			role = "admin"
		}
		tid := tu.ID
		u = &store.User{TelegramID: &tid, Username: displayName(tu), Role: role}
		if err := a.store.CreateUser(u); err != nil {
			a.errJSON(w, http.StatusInternalServerError, "cannot create user")
			return
		}
		a.log.Info("user registered via mini app", "user_id", u.ID, "telegram_id", tu.ID)
	default:
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	if u.Disabled {
		a.errJSON(w, http.StatusForbidden, "account disabled")
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
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "devices": out, "max_devices": a.cfg.MaxDevices})
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
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := a.provisionDevice(id.userID, sanitizeName(body.Name, fmt.Sprintf("device-%d", len(existing)+1)))
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
		PublicKey string   `json:"public_key"`
		Port      int      `json:"port"`
		Host      string   `json:"host"`
		Subnet    string   `json:"subnet"`
		DNS       []string `json:"dns"`
		MTU       int      `json:"mtu"`
		Passive   bool     `json:"passive"`
	} `json:"server"`
	Totals struct {
		Users   int   `json:"users"`
		Devices int   `json:"devices"`
		Online  int   `json:"online"`
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

	users, err := a.store.ListUsers()
	if err != nil {
		return nil, err
	}
	devices, err := a.store.ListDevices(0)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Username
	}
	ov.Totals.Users = len(users)
	ov.Totals.Devices = len(devices)
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
}

func (a *API) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
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
	stats := make(map[int64]*userOut, len(users))
	out := make([]userOut, 0, len(users))
	for _, u := range users {
		s := userOut{User: u}
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
	var body struct {
		TelegramID *int64 `json:"telegram_id"`
		Username   string `json:"username"`
		Role       string `json:"role"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Role != "" && body.Role != "user" && body.Role != "admin" {
		a.errJSON(w, http.StatusBadRequest, "role must be user or admin")
		return
	}
	u := &store.User{TelegramID: body.TelegramID, Username: sanitizeName(body.Username, ""), Role: body.Role}
	if err := a.store.CreateUser(u); err != nil {
		a.errJSON(w, http.StatusConflict, "user already exists")
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (a *API) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return
	}
	var body struct {
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Role != nil && *body.Role != "user" && *body.Role != "admin" {
		a.errJSON(w, http.StatusBadRequest, "role must be user or admin")
		return
	}
	if body.Role != nil {
		if err := a.store.SetUserRole(id, *body.Role); err != nil {
			a.errJSON(w, http.StatusNotFound, "user not found")
			return
		}
	}
	if body.Disabled != nil {
		if err := a.store.SetUserDisabled(id, *body.Disabled); err != nil {
			a.errJSON(w, http.StatusNotFound, "user not found")
			return
		}
	}
	u, err := a.store.GetUser(id)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *API) handleAdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := a.store.DeleteUser(id); err != nil {
		a.errJSON(w, http.StatusNotFound, "user not found")
		return
	}
	a.syncPeers() // their devices cascade-deleted, peers must go too
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminCreateDevice provisions a device for any user (works for manual
// panel-created users who cannot use the Mini App login).
func (a *API) handleAdminCreateDevice(w http.ResponseWriter, r *http.Request) {
	uid, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return
	}
	if _, err := a.store.GetUser(uid); err != nil {
		a.errJSON(w, http.StatusNotFound, "user not found")
		return
	}
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
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := a.provisionDevice(uid, sanitizeName(body.Name, fmt.Sprintf("device-%d", len(existing)+1)))
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, newDeviceOut(*d, ""))
}

// provisionDevice generates keys and an address and stores the device.
func (a *API) provisionDevice(userID int64, name string) (*store.Device, error) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("key generation failed")
	}
	psk, err := wg.GeneratePresharedKey()
	if err != nil {
		return nil, fmt.Errorf("key generation failed")
	}
	ip, err := a.store.NextIPv4(a.cfg.Subnet)
	if err != nil {
		return nil, fmt.Errorf("no free addresses in subnet")
	}
	d := &store.Device{
		UserID:       userID,
		Name:         name,
		PublicKey:    priv.PublicKey().String(),
		PrivateKey:   priv.String(),
		PresharedKey: psk.String(),
		IPv4:         ip,
		Enabled:      true,
	}
	if err := a.store.CreateDevice(d); err != nil {
		return nil, fmt.Errorf("cannot create device")
	}
	a.syncPeers()
	return d, nil
}

func (a *API) handleAdminDevices(w http.ResponseWriter, r *http.Request) {
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
	names := make(map[int64]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Username
	}
	out := make([]deviceOut, 0, len(devices))
	for _, d := range devices {
		out = append(out, newDeviceOut(d, names[d.UserID]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleAdminDeviceConfig(w http.ResponseWriter, r *http.Request) {
	d, ok := a.anyDevice(w, r)
	if !ok {
		return
	}
	a.serveConfig(w, *d)
}

func (a *API) handleAdminDeviceQR(w http.ResponseWriter, r *http.Request) {
	d, ok := a.anyDevice(w, r)
	if !ok {
		return
	}
	a.serveQR(w, *d)
}

func (a *API) handleAdminUpdateDevice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return
	}
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
	}
	d, err := a.store.GetDevice(id)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, newDeviceOut(*d, ""))
}

func (a *API) handleAdminDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := a.store.DeleteDevice(id); err != nil {
		a.errJSON(w, http.StatusNotFound, "device not found")
		return
	}
	a.syncPeers()
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
		PublicKey: d.PublicKey, IPv4: d.IPv4, Enabled: d.Enabled,
		Online: isOnline(d), CreatedAt: d.CreatedAt,
		TotalRx: d.TotalRx, TotalTx: d.TotalTx,
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

func (a *API) anyDevice(w http.ResponseWriter, r *http.Request) (*store.Device, bool) {
	id, err := pathID(r)
	if err != nil {
		a.errJSON(w, http.StatusBadRequest, "bad id")
		return nil, false
	}
	d, err := a.store.GetDevice(id)
	if err != nil {
		a.errJSON(w, http.StatusNotFound, "device not found")
		return nil, false
	}
	return d, true
}

func (a *API) serveConfig(w http.ResponseWriter, d store.Device) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="fibr-%s.conf"`, fileSafe(d.Name)))
	w.Write([]byte(a.wg.ClientConfig(d)))
}

func (a *API) serveQR(w http.ResponseWriter, d store.Device) {
	png, err := qrcode.Encode(a.wg.ClientConfig(d), qrcode.Medium, 512)
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
