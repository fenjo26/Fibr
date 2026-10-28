package api

// Per-user device caps with owner cascade (mirrors the expires_at billing
// semantics; see docs/roles.md): who may set caps, what a capped account's
// effective limit is, and the 409 at the limit on both device-creation
// endpoints.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fenjo26/Fibr/internal/store"
)

// TestPatchUserMaxDevices: PATCH max_devices tri-state — the superadmin sets
// anything, an admin only values within their own effective cap (403 above,
// null always allowed), absent leaves it alone, junk is a 400.
func TestPatchUserMaxDevices(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)
	own := "/api/admin/users/" + itoa(h.ownUser.ID)

	// The reseller caps themself to 3 first (via the superadmin).
	rec := adminReq(a, http.MethodPatch, "/api/admin/users/"+itoa(h.reseller.ID), `{"max_devices":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("superadmin setting admin cap: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ := st.GetUser(h.reseller.ID)
	if got.MaxDevices == nil || *got.MaxDevices != 3 {
		t.Fatalf("admin cap not persisted: %+v", got.MaxDevices)
	}

	// The admin sets a cap within their own: fine.
	rec = sessReq(a, h.resTok, http.MethodPatch, own, `{"max_devices":2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin capping own user within cap: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ = st.GetUser(h.ownUser.ID)
	if got.MaxDevices == nil || *got.MaxDevices != 2 {
		t.Fatalf("user cap not persisted: %+v", got.MaxDevices)
	}

	// Above their own cap: 403 with the exact reason.
	rec = sessReq(a, h.resTok, http.MethodPatch, own, `{"max_devices":4}`)
	if rec.Code != http.StatusForbidden || errMessage(t, rec) != "device limit exceeds your own" {
		t.Fatalf("admin exceeding own cap: status %d body %s", rec.Code, rec.Body.String())
	}

	// Null (reset to global default) is always allowed for a scoped user:
	// the owner cascade still bounds them at 3.
	rec = sessReq(a, h.resTok, http.MethodPatch, own, `{"max_devices":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin clearing user cap: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ = st.GetUser(h.ownUser.ID)
	if got.MaxDevices != nil {
		t.Fatalf("null must clear the cap, got %+v", got.MaxDevices)
	}

	// The superadmin is uncapped: any value sticks.
	rec = adminReq(a, http.MethodPatch, own, `{"max_devices":0}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("superadmin setting 0: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ = st.GetUser(h.ownUser.ID)
	if got.MaxDevices == nil || *got.MaxDevices != 0 {
		t.Fatalf("0 must persist, got %+v", got.MaxDevices)
	}

	// An unrelated PATCH (disabled) must not clobber the cap.
	rec = adminReq(a, http.MethodPatch, own, `{"disabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch disabled: status %d body %s", rec.Code, rec.Body.String())
	}
	got, _ = st.GetUser(h.ownUser.ID)
	if got.MaxDevices == nil {
		t.Fatal("absent max_devices must leave the cap alone")
	}

	// Junk values stay 400.
	for _, body := range []string{`{"max_devices":-1}`, `{"max_devices":"three"}`, `{"max_devices":1.5}`} {
		if rec := adminReq(a, http.MethodPatch, own, body); rec.Code != http.StatusBadRequest {
			t.Fatalf("bad max_devices %s: status %d body %s", body, rec.Code, rec.Body.String())
		}
	}

	// The patched response echoes the cap.
	rec = adminReq(a, http.MethodPatch, own, `{"max_devices":6}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-set cap: status %d", rec.Code)
	}
	var patched store.User
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil || patched.MaxDevices == nil || *patched.MaxDevices != 6 {
		t.Fatalf("max_devices missing from response: %v %s", err, rec.Body.String())
	}
}

// TestDeviceLimitCascade: the effective cap is min(own, owner's) over the
// set ones, global default last; both POST /api/me/devices and the admin
// device-create enforce it with the same 409.
func TestDeviceLimitCascade(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)
	// The seeded ownUser already has one device (ownDev).
	ownTok := a.newSessionToken(h.ownUser.ID)

	create := func(tok, path string) int {
		t.Helper()
		rec := sessReq(a, tok, http.MethodPost, path, `{"name":"x","protocol":"wg"}`)
		return rec.Code
	}

	// Global default 10 applies while nothing is set.
	if c := create(ownTok, "/api/me/devices"); c != http.StatusCreated {
		t.Fatalf("uncapped create: %d", c)
	}

	// Owner cascade alone: admin capped to 2, user uncapped -> effective 2,
	// and the user already holds 2 devices -> 409.
	two := int64(2)
	if err := st.SetUserMaxDevices(h.reseller.ID, &two); err != nil {
		t.Fatalf("cap admin: %v", err)
	}
	if c := create(ownTok, "/api/me/devices"); c != http.StatusConflict {
		t.Fatalf("capped-by-owner create must 409, got %d", c)
	}
	// The same limit gates the admin's device-create for that user.
	if c := create(h.resTok, "/api/admin/users/"+itoa(h.ownUser.ID)+"/devices"); c != http.StatusConflict {
		t.Fatalf("admin create under cascade cap must 409, got %d", c)
	}

	// min(own, owner): own=1 is tighter than owner=2 -> effective 1; the
	// user already has 2 devices so even after the owner relaxes, own wins.
	one := int64(1)
	if err := st.SetUserMaxDevices(h.ownUser.ID, &one); err != nil {
		t.Fatalf("cap user: %v", err)
	}
	if err := st.SetUserMaxDevices(h.reseller.ID, nil); err != nil {
		t.Fatalf("relax admin: %v", err)
	}
	if c := create(ownTok, "/api/me/devices"); c != http.StatusConflict {
		t.Fatalf("own-tighter cap must 409, got %d", c)
	}

	// 0 = "no new devices" is a real value.
	zero := int64(0)
	if err := st.SetUserMaxDevices(h.ownUser.ID, &zero); err != nil {
		t.Fatalf("zero cap: %v", err)
	}
	if c := create(ownTok, "/api/me/devices"); c != http.StatusConflict {
		t.Fatalf("zero cap must 409, got %d", c)
	}

	// Clearing the cap restores the global default.
	if err := st.SetUserMaxDevices(h.ownUser.ID, nil); err != nil {
		t.Fatalf("clear cap: %v", err)
	}
	if c := create(ownTok, "/api/me/devices"); c != http.StatusCreated {
		t.Fatalf("restored create: %d", c)
	}

	// A user with no owner is governed by own-or-default only: a set value
	// above the global default widens (no implicit min with the fallback).
	big := int64(11)
	if err := st.SetUserMaxDevices(h.loner.ID, &big); err != nil {
		t.Fatalf("cap loner: %v", err)
	}
	lonerTok := a.newSessionToken(h.loner.ID)
	for i := 0; i < 11; i++ {
		if c := create(lonerTok, "/api/me/devices"); c != http.StatusCreated {
			t.Fatalf("loner create %d past the global default must work (own cap 11), got %d", i+1, c)
		}
	}
	if c := create(lonerTok, "/api/me/devices"); c != http.StatusConflict {
		t.Fatalf("loner create 12 must 409 (own cap 11), got %d", c)
	}
}

// TestMeExposesEffectiveLimit: /api/me returns the account's effective cap
// (never the raw global default when a cascade applies) plus the multi-node
// endpoint fields.
func TestMeExposesEffectiveLimit(t *testing.T) {
	a, st := newTestAPI(t, false)
	h := seedHierarchy(t, a, st)

	// Own cap set: effective = own.
	three := int64(3)
	if err := st.SetUserMaxDevices(h.ownUser.ID, &three); err != nil {
		t.Fatalf("cap: %v", err)
	}
	rec := sessReq(a, a.newSessionToken(h.ownUser.ID), http.MethodGet, "/api/me", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/me: status %d body %s", rec.Code, rec.Body.String())
	}
	var me struct {
		MaxDevices        int    `json:"max_devices"`
		ActiveNode        string `json:"active_node"`
		EndpointChangedAt int64  `json:"endpoint_changed_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("bad json: %v %s", err, rec.Body.String())
	}
	if me.MaxDevices != 3 {
		t.Fatalf("effective limit must be 3 (own), got %d", me.MaxDevices)
	}
	if me.ActiveNode == "" {
		t.Fatal("active_node must be present in /api/me")
	}

	// Owner cascade: admin capped tighter than the user's own value.
	one := int64(1)
	if err := st.SetUserMaxDevices(h.ownUser.ID, nil); err != nil {
		t.Fatalf("clear user cap: %v", err)
	}
	if err := st.SetUserMaxDevices(h.reseller.ID, &one); err != nil {
		t.Fatalf("cap admin: %v", err)
	}
	rec = sessReq(a, a.newSessionToken(h.ownUser.ID), http.MethodGet, "/api/me", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if me.MaxDevices != 1 {
		t.Fatalf("effective limit must be 1 (owner cascade), got %d", me.MaxDevices)
	}

	// An admin's own /api/me resolves to own-or-default (no cascade: only a
	// superadmin can own an admin, and superadmins are unlimited).
	rec = sessReq(a, h.resTok, http.MethodGet, "/api/me", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if me.MaxDevices != 1 {
		t.Fatalf("admin effective limit must be their own 1, got %d", me.MaxDevices)
	}
}
