package api

// Reseller-hierarchy tests: what an admin (sub-admin/reseller) may see and
// do versus the superadmin (static token / FIBR_ADMIN_TELEGRAM_IDS), per
// docs/roles.md.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fenjo26/Fibr/internal/store"
)

// sessReq fires a request authenticated with a session token of the given
// user (the reseller/superadmin path — adminReq in invites_test.go covers
// the static token).
func sessReq(a *API, token, method, path, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

// seedHierarchy builds the cast: two resellers, each with one owned user, a
// user of nobody, and one device per owned user. Returns session tokens.
type hierarchy struct {
	reseller   *store.User
	foreign    *store.User
	ownUser    *store.User
	foreignUsr *store.User
	loner      *store.User
	ownDev     *store.Device
	foreignDev *store.Device
	resTok     string
}

func seedHierarchy(t *testing.T, a *API, st *store.Store) *hierarchy {
	t.Helper()
	mustUser := func(u *store.User) *store.User {
		if err := st.CreateUser(u); err != nil {
			t.Fatalf("seed user %s: %v", u.Username, err)
		}
		return u
	}
	res := mustUser(&store.User{Username: "reseller", Role: store.RoleAdmin})
	frn := mustUser(&store.User{Username: "foreign-reseller", Role: store.RoleAdmin})
	own := mustUser(&store.User{Username: "owned-by-reseller", Role: store.RoleUser, OwnerID: &res.ID})
	fuw := mustUser(&store.User{Username: "owned-by-foreign", Role: store.RoleUser, OwnerID: &frn.ID})
	lon := mustUser(&store.User{Username: "loner", Role: store.RoleUser})

	mustDev := func(uid int64, name, ip string) *store.Device {
		d := &store.Device{UserID: uid, Name: name, PublicKey: "pub-" + name, PrivateKey: "priv-" + name, IPv4: ip, Enabled: true}
		if err := st.CreateDevice(d); err != nil {
			t.Fatalf("seed device %s: %v", name, err)
		}
		return d
	}
	return &hierarchy{
		reseller: res, foreign: frn, ownUser: own, foreignUsr: fuw, loner: lon,
		ownDev:     mustDev(own.ID, "owned-phone", "10.8.0.2"),
		foreignDev: mustDev(fuw.ID, "foreign-phone", "10.8.0.3"),
		resTok:     a.newSessionToken(res.ID),
	}
}

// TestAdminSeesOnlySubtree: the users list contains only the admin's own
// accounts (not themself, not other resellers' users, not self-registered
// ones), and creation is forced to role=user owned by the admin.
func TestAdminSeesOnlySubtree(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)

	rec := sessReq(a, h.resTok, http.MethodGet, "/api/admin/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list users: status %d body %s", rec.Code, rec.Body.String())
	}
	var users []store.User
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(users) != 1 || users[0].ID != h.ownUser.ID {
		t.Fatalf("admin must see exactly their own user, got %+v", users)
	}
	if users[0].OwnerID == nil || *users[0].OwnerID != h.reseller.ID {
		t.Fatalf("owner_id missing from list row: %+v", users[0])
	}

	// Creating an admin must be rejected with 400.
	rec = sessReq(a, h.resTok, http.MethodPost, "/api/admin/users", `{"username":"mini-me","role":"admin"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("admin creating admin: status %d body %s", rec.Code, rec.Body.String())
	}
	rec = sessReq(a, h.resTok, http.MethodPost, "/api/admin/users", `{"username":"mini-me","role":"superadmin"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("admin creating superadmin: status %d body %s", rec.Code, rec.Body.String())
	}

	// A plain user creation works and records the creator as owner.
	rec = sessReq(a, h.resTok, http.MethodPost, "/api/admin/users", `{"username":"mini-me"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin creating user: status %d body %s", rec.Code, rec.Body.String())
	}
	var created store.User
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if created.Role != store.RoleUser {
		t.Fatalf("created role must be forced to user, got %q", created.Role)
	}
	if created.OwnerID == nil || *created.OwnerID != h.reseller.ID {
		t.Fatalf("created user must be owned by the admin: %+v", created)
	}
}

// TestAdminUserManagementScope: PATCH/DELETE on foreign users read as 404,
// role changes are refused, own users can be disabled and deleted.
func TestAdminUserManagementScope(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)

	foreign := "/api/admin/users/" + itoa(h.foreignUsr.ID)
	own := "/api/admin/users/" + itoa(h.ownUser.ID)

	// Foreign user: invisible for both disable and delete.
	if rec := sessReq(a, h.resTok, http.MethodPatch, foreign, `{"disabled":true}`); rec.Code != http.StatusNotFound {
		t.Fatalf("patch foreign user: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := sessReq(a, h.resTok, http.MethodDelete, foreign, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete foreign user: status %d body %s", rec.Code, rec.Body.String())
	}

	// Own user: role change refused (superadmin only), disable fine.
	if rec := sessReq(a, h.resTok, http.MethodPatch, own, `{"role":"admin"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("admin promoting own user: status %d body %s", rec.Code, rec.Body.String())
	}
	rec := sessReq(a, h.resTok, http.MethodPatch, own, `{"disabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable own user: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ := st.GetUser(h.ownUser.ID)
	if !got.Disabled {
		t.Fatal("disable not persisted")
	}

	// The admin themself is outside their subtree: self-delete reads 404 too.
	self := "/api/admin/users/" + itoa(h.reseller.ID)
	if rec := sessReq(a, h.resTok, http.MethodDelete, self, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("admin deleting themself: status %d body %s", rec.Code, rec.Body.String())
	}

	// Delete of an own user cascades their devices.
	if rec := sessReq(a, h.resTok, http.MethodDelete, own, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete own user: status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := st.GetDevice(h.ownDev.ID); err != store.ErrNotFound {
		t.Fatalf("own user devices must cascade, got %v", err)
	}
}

// TestAdminDeviceScope: an admin may issue devices to their users and manage
// those; anything belonging to another reseller's user reads as 404.
func TestAdminDeviceScope(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)

	// Issue a device to their own user: 201 with a QR available right away.
	own := "/api/admin/users/" + itoa(h.ownUser.ID) + "/devices"
	rec := sessReq(a, h.resTok, http.MethodPost, own, `{"name":"work-laptop","protocol":"wg"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("issue device to own user: status %d body %s", rec.Code, rec.Body.String())
	}
	var dev deviceOut
	if err := json.Unmarshal(rec.Body.Bytes(), &dev); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if dev.UserID != h.ownUser.ID || dev.Protocol != "wg" || dev.PublicKey == "" {
		t.Fatalf("issued device mismatch: %+v", dev)
	}

	// Foreign user: no device can be issued or touched.
	foreignUser := "/api/admin/users/" + itoa(h.foreignUsr.ID) + "/devices"
	if rec := sessReq(a, h.resTok, http.MethodPost, foreignUser, `{"name":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("issue device to foreign user: status %d body %s", rec.Code, rec.Body.String())
	}
	fd := "/api/admin/devices/" + itoa(h.foreignDev.ID)
	if rec := sessReq(a, h.resTok, http.MethodPatch, fd, `{"enabled":false}`); rec.Code != http.StatusNotFound {
		t.Fatalf("patch foreign device: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := sessReq(a, h.resTok, http.MethodDelete, fd, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete foreign device: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := sessReq(a, h.resTok, http.MethodGet, fd+"/config", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign device config: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := sessReq(a, h.resTok, http.MethodGet, fd+"/qr.png", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("foreign device qr: status %d body %s", rec.Code, rec.Body.String())
	}

	// The devices list contains only subtree devices.
	rec = sessReq(a, h.resTok, http.MethodGet, "/api/admin/devices", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list devices: status %d", rec.Code)
	}
	var devices []deviceOut
	if err := json.Unmarshal(rec.Body.Bytes(), &devices); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	for _, d := range devices {
		if d.UserID == h.foreignUsr.ID {
			t.Fatalf("foreign device leaked into admin list: %+v", d)
		}
	}
	if len(devices) != 2 { // ownUser's seeded phone + the issued laptop
		t.Fatalf("admin must see exactly their subtree devices, got %+v", devices)
	}
}

// TestAdminOverviewForbidden: installation-wide metrics are the owner's
// alone; an admin session gets 403 while the static token sees them.
func TestAdminOverviewForbidden(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)

	if rec := sessReq(a, h.resTok, http.MethodGet, "/api/admin/overview", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("admin overview must be 403, got %d body %s", rec.Code, rec.Body.String())
	}
	if rec := adminReq(a, http.MethodGet, "/api/admin/overview", ""); rec.Code != http.StatusOK {
		t.Fatalf("superadmin overview: status %d body %s", rec.Code, rec.Body.String())
	}
}

// TestAdminInvitesScope: both roles may create invites; the list and DELETE
// are scoped — an admin only manages their own codes.
func TestAdminInvitesScope(t *testing.T) {
	a, _ := newTestAPI(t, false)
	h := seedHierarchy(t, a, a.store)

	// Both create one invite.
	rec := sessReq(a, h.resTok, http.MethodPost, "/api/admin/invites", `{"note":"reseller"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin invite create: status %d body %s", rec.Code, rec.Body.String())
	}
	var own inviteOut
	if err := json.Unmarshal(rec.Body.Bytes(), &own); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	rec = adminReq(a, http.MethodPost, "/api/admin/invites", `{"note":"static-token"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("superadmin invite create: status %d", rec.Code)
	}
	var tokenInvite inviteOut
	if err := json.Unmarshal(rec.Body.Bytes(), &tokenInvite); err != nil {
		t.Fatalf("bad json: %v", err)
	}

	// The admin's list shows only their code; the superadmin sees both.
	rec = sessReq(a, h.resTok, http.MethodGet, "/api/admin/invites", "")
	var list []inviteOut
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 || list[0].Code != own.Code {
		t.Fatalf("admin invite list: %v %s", list, rec.Body.String())
	}
	rec = adminReq(a, http.MethodGet, "/api/admin/invites", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("superadmin invite list: %v %s", list, rec.Body.String())
	}

	// The admin cannot delete the token's invite (404), their own — can.
	if rec := sessReq(a, h.resTok, http.MethodDelete, "/api/admin/invites/"+tokenInvite.Code, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("admin deleting foreign invite: status %d body %s", rec.Code, rec.Body.String())
	}
	if rec := sessReq(a, h.resTok, http.MethodDelete, "/api/admin/invites/"+own.Code, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("admin deleting own invite: status %d body %s", rec.Code, rec.Body.String())
	}
}

// TestSuperadminManagesEverything: the static token sees all users, promotes
// user→admin, demotes back and deletes across reseller boundaries.
func TestSuperadminManagesEverything(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)

	rec := adminReq(a, http.MethodGet, "/api/admin/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d", rec.Code)
	}
	var users []store.User
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(users) != 5 {
		t.Fatalf("superadmin must see all 5 users, got %d", len(users))
	}

	// Promote a plain user to admin (reseller).
	rec = adminReq(a, http.MethodPatch, "/api/admin/users/"+itoa(h.ownUser.ID), `{"role":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("promote: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ := st.GetUser(h.ownUser.ID)
	if got.Role != store.RoleAdmin {
		t.Fatalf("role not promoted: %s", got.Role)
	}

	// And demote back.
	rec = adminReq(a, http.MethodPatch, "/api/admin/users/"+itoa(h.ownUser.ID), `{"role":"user"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("demote: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ = st.GetUser(h.ownUser.ID)
	if got.Role != store.RoleUser {
		t.Fatalf("role not demoted: %s", got.Role)
	}

	// Invalid role values stay 400.
	if rec := adminReq(a, http.MethodPatch, "/api/admin/users/"+itoa(h.ownUser.ID), `{"role":"wizard"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid role: status %d", rec.Code)
	}

	// Devices across boundaries are manageable (incl. foreign device QR).
	if rec := adminReq(a, http.MethodGet, "/api/admin/devices/"+itoa(h.foreignDev.ID)+"/qr.png", ""); rec.Code != http.StatusOK {
		t.Fatalf("superadmin foreign qr: status %d", rec.Code)
	}

	// Delete a foreign user.
	if rec := adminReq(a, http.MethodDelete, "/api/admin/users/"+itoa(h.foreignUsr.ID), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("superadmin delete foreign user: status %d body %s", rec.Code, rec.Body.String())
	}
}

// TestTelegramSuperadminUpgrade: FIBR_ADMIN_TELEGRAM_IDS now means
// superadmin — existing 'admin' rows are upgraded on sign-in, fresh
// registrations start as superadmin, everyone else stays a user.
func TestTelegramSuperadminUpgrade(t *testing.T) {
	a, st := newTestAPI(t, false)
	a.cfg.TelegramBotToken = testBotToken
	a.cfg.AdminTelegramIDs = []int64{901, 903}

	// Legacy account: role 'admin' from before the three-role model, same
	// telegram id as an env-listed superadmin.
	tid := int64(901)
	legacy := &store.User{TelegramID: &tid, Username: "legacy", Role: store.RoleAdmin}
	if err := st.CreateUser(legacy); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	rec := authPOST(a, signedInitData(901, "legacy"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy sign-in: status %d body %s", rec.Code, rec.Body.String())
	}
	var sess struct {
		User *store.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil || sess.User == nil {
		t.Fatalf("bad session json: %v: %s", err, rec.Body.String())
	}
	if sess.User.Role != store.RoleSuperadmin {
		t.Fatalf("legacy 'admin' must upgrade to superadmin, got %q", sess.User.Role)
	}
	got, _ := st.GetUserByTelegramID(901)
	if got.Role != store.RoleSuperadmin {
		t.Fatalf("upgrade not persisted: %s", got.Role)
	}

	// Fresh registration of an env-listed id -> superadmin right away.
	rec = authPOST(a, signedInitData(903, "envadmin"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in: status %d body %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil || sess.User == nil {
		t.Fatalf("bad session json: %v: %s", err, rec.Body.String())
	}
	if sess.User.Role != store.RoleSuperadmin {
		t.Fatalf("env-listed id must register as superadmin, got %q", sess.User.Role)
	}

	// A non-listed id stays a plain user.
	rec = authPOST(a, signedInitData(902, "peasant"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("peasant sign-in: status %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if sess.User.Role != store.RoleUser {
		t.Fatalf("non-listed id must stay user, got %q", sess.User.Role)
	}
}

// TestSuperadminSessionSeesAll: a superadmin identified by a Mini App session
// (not the static token) gets the same unrestricted view.
func TestSuperadminSessionSeesAll(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)

	// Promote the reseller to superadmin and re-auth with a fresh session.
	if err := st.SetUserRole(h.reseller.ID, store.RoleSuperadmin); err != nil {
		t.Fatalf("promote: %v", err)
	}
	superTok := a.newSessionToken(h.reseller.ID)

	rec := sessReq(a, superTok, http.MethodGet, "/api/admin/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d", rec.Code)
	}
	var users []store.User
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(users) != 5 {
		t.Fatalf("superadmin session must see all users, got %d", len(users))
	}
	if rec := sessReq(a, superTok, http.MethodGet, "/api/admin/overview", ""); rec.Code != http.StatusOK {
		t.Fatalf("overview: status %d", rec.Code)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }
