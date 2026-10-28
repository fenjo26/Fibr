package api

// Self-enrollment invites (stage 4): the admin generates one-time codes in
// the panel, people follow https://<domain>/miniapp/?invite=CODE (or a
// t.me/<bot>?startapp=CODE deep link) and register themselves in the Mini
// App. See docs/self-enroll.md.

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"strings"

	"github.com/fenjo26/Fibr/internal/store"
)

// inviteAlphabet is URL-safe and has the look-alike characters removed
// (0/O/o, 1/l/I/i) so codes survive being read aloud or retyped.
const inviteAlphabet = "23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ"

const inviteCodeLen = 10

// GenerateInviteCode returns a crypto/rand code of inviteCodeLen chars from
// inviteAlphabet (53^10 ≈ 1.6e17 combinations). Exported for the bot's
// /invite command.
func GenerateInviteCode() (string, error) {
	var b strings.Builder
	b.Grow(inviteCodeLen)
	max := big.NewInt(int64(len(inviteAlphabet)))
	for i := 0; i < inviteCodeLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(inviteAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// InviteLink renders the enrollment link for a code. With a real public
// domain it is absolute https; in dev (empty domain / localhost) it stays a
// relative path the admin panel can still copy.
func InviteLink(domain, code string) string {
	d := strings.TrimSpace(domain)
	if i := strings.Index(d, "://"); i >= 0 { // dev mode: FIBR_DOMAIN=http://localhost
		d = d[i+3:]
	}
	d = strings.TrimSuffix(strings.TrimSpace(d), "/")
	if d == "" || d == "localhost" || strings.HasPrefix(d, "localhost:") ||
		d == "127.0.0.1" || strings.HasPrefix(d, "127.0.0.1:") {
		return "/miniapp/?invite=" + code
	}
	return "https://" + d + "/miniapp/?invite=" + code
}

// inviteOut is the JSON shape of an invite in the admin panel, with the
// username of the consuming user joined in memory (same pattern as devices).
type inviteOut struct {
	store.Invite
	Link     string `json:"link"`
	Username string `json:"username,omitempty"` // who consumed the code, if anyone
}

func (a *API) handleAdminListInvites(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	invites, err := a.store.ListInvites()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	users, err := a.store.ListUsers()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	names := make(map[int64]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Username
	}
	out := make([]inviteOut, 0, len(invites))
	for _, inv := range invites {
		// Reseller scope: an admin only sees invites they generated
		// (created_by = their id); the superadmin sees all, including the
		// ones minted with the static token (created_by = 0).
		if !id.isSuper() && inv.CreatedBy != id.userID {
			continue
		}
		o := inviteOut{Invite: inv, Link: InviteLink(a.cfg.Domain, inv.Code)}
		if inv.UsedBy != nil {
			o.Username = names[*inv.UsedBy]
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleAdminCreateInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Note string `json:"note"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	id := ident(r)
	inv := &store.Invite{Note: strings.TrimSpace(body.Note), CreatedBy: id.userID}
	// The alphabet is huge, so collisions are ~impossible; retry anyway so a
	// duplicate PRIMARY KEY can never surface as a 500.
	for attempt := 0; attempt < 5; attempt++ {
		code, err := GenerateInviteCode()
		if err != nil {
			a.errJSON(w, http.StatusInternalServerError, "cannot generate invite code")
			return
		}
		inv.Code = code
		if err := a.store.CreateInvite(inv); err == nil {
			a.log.Info("invite created", "code", inv.Code, "created_by", inv.CreatedBy)
			writeJSON(w, http.StatusCreated, inviteOut{Invite: *inv, Link: InviteLink(a.cfg.Domain, inv.Code)})
			return
		}
	}
	a.errJSON(w, http.StatusInternalServerError, "cannot generate invite code")
}

func (a *API) handleAdminDeleteInvite(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		a.errJSON(w, http.StatusBadRequest, "bad code")
		return
	}
	id := ident(r)
	if !id.isSuper() {
		// An admin may only delete their own invites; foreign codes read as
		// 404 so ids cannot be probed.
		inv, err := a.store.GetInvite(code)
		if err != nil || inv.CreatedBy != id.userID {
			a.errJSON(w, http.StatusNotFound, "invite not found")
			return
		}
	}
	if err := a.store.DeleteInvite(code); err != nil {
		a.errJSON(w, http.StatusNotFound, "invite not found")
		return
	}
	a.log.Info("invite deleted", "code", code)
	w.WriteHeader(http.StatusNoContent)
}

// envBool has moved to internal/config (Config.RequireInvite).
