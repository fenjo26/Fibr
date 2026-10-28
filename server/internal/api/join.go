package api

// Quick-connect join codes: the owner of a device mints a short one-time
// code in the Mini App (or an admin in the panel); the client holding the
// code exchanges it for the rendered config via the public
// POST /api/join/{code}. The https://<domain>/join/<code> page (GET, also
// public) never consumes anything — it only explains and offers a
// fibr://join/<...> deep link — so messenger link previews cannot burn the
// code. See docs/join.md.

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"html/template"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/fenjo26/Fibr/internal/store"
)

// joinCodeLen is deliberately shorter than invites: the code is typed by
// hand into the client ("По коду" tab). 53^8 ≈ 6.2e13 combinations with a
// 15-minute TTL window is far beyond any guessing throughput.
const joinCodeLen = 8

func generateJoinCode() (string, error) {
	var b strings.Builder
	b.Grow(joinCodeLen)
	max := big.NewInt(int64(len(inviteAlphabet)))
	for i := 0; i < joinCodeLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(inviteAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// JoinLink renders the quick-connect page link for a code. Same dev-mode
// convention as InviteLink: without a real public domain it stays relative.
func JoinLink(domain, code string) string {
	d := strings.TrimSpace(domain)
	if i := strings.Index(d, "://"); i >= 0 { // dev mode: FIBR_DOMAIN=http://localhost
		d = d[i+3:]
	}
	d = strings.TrimSuffix(strings.TrimSpace(d), "/")
	if d == "" || d == "localhost" || strings.HasPrefix(d, "localhost:") ||
		d == "127.0.0.1" || strings.HasPrefix(d, "127.0.0.1:") {
		return "/join/" + code
	}
	return "https://" + d + "/join/" + code
}

// joinOut is the JSON shape returned when a code is minted.
type joinOut struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	DeviceID  int64     `json:"device_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// issueJoinCode mints a code for an already-scoped device. Shared by the
// /api/me and /api/admin endpoints; the difference is only the access check
// done by the caller.
func (a *API) issueJoinCode(w http.ResponseWriter, d *store.Device, creator int64) {
	_ = a.store.DeleteStaleJoinCodes() // best-effort housekeeping
	for attempt := 0; attempt < 5; attempt++ {
		code, err := generateJoinCode()
		if err != nil {
			a.errJSON(w, http.StatusInternalServerError, "cannot generate join code")
			return
		}
		jc := &store.JoinCode{
			Code:      code,
			DeviceID:  d.ID,
			CreatedBy: creator,
			ExpiresAt: time.Now().Add(a.cfg.JoinTTL),
		}
		if err := a.store.CreateJoinCode(jc); err == nil {
			a.log.Info("join code created", "code", jc.Code, "device_id", d.ID, "created_by", creator)
			writeJSON(w, http.StatusCreated, joinOut{
				Code:      jc.Code,
				URL:       JoinLink(a.cfg.Domain, jc.Code),
				DeviceID:  d.ID,
				ExpiresAt: jc.ExpiresAt,
			})
			return
		}
	}
	a.errJSON(w, http.StatusInternalServerError, "cannot generate join code")
}

// handleMeJoinCode mints a quick-connect code for the caller's own device.
func (a *API) handleMeJoinCode(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	d, ok := a.ownedDevice(w, r, id.userID)
	if !ok {
		return
	}
	a.issueJoinCode(w, d, id.userID)
}

// handleAdminJoinCode mints a quick-connect code for a device inside the
// admin's subtree (a reseller handing a config to their customer).
func (a *API) handleAdminJoinCode(w http.ResponseWriter, r *http.Request) {
	d, ok := a.scopedDevice(w, r)
	if !ok {
		return
	}
	a.issueJoinCode(w, d, ident(r).userID)
}

// joinResult is the JSON payload the client receives for a valid code: the
// rendered credential plus enough metadata to name the tunnel.
type joinResult struct {
	DeviceID int64  `json:"device_id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Config   string `json:"config"`
}

// handleJoin is the public exchange: one code → one rendered config. The
// code is burned atomically before the config is rendered, so a code can be
// redeemed exactly once even under races.
func (a *API) handleJoin(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		a.errJSON(w, http.StatusBadRequest, "bad code")
		return
	}
	jc, err := a.store.ConsumeJoinCode(code)
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.errJSON(w, http.StatusNotFound, "code not found")
		return
	case errors.Is(err, store.ErrJoinCodeUsed):
		a.errJSON(w, http.StatusGone, "code already used or expired")
		return
	case err != nil:
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}

	d, err := a.store.GetDevice(jc.DeviceID)
	if err != nil {
		// The device vanished after the code was minted (FK cascade deletes
		// codes, but a race is possible). The code stays burned.
		a.errJSON(w, http.StatusNotFound, "code not found")
		return
	}
	owner, err := a.store.GetUser(d.UserID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	if owner.Disabled {
		a.errJSON(w, http.StatusForbidden, "account disabled")
		return
	}
	if a.userExpired(owner) {
		a.errJSON(w, http.StatusForbidden, msgSubscriptionExpired)
		return
	}
	if !d.Enabled {
		a.errJSON(w, http.StatusForbidden, "device disabled")
		return
	}

	var config string
	switch d.Protocol {
	case store.ProtocolXray:
		if !a.cfg.XrayEnabled {
			a.errJSON(w, http.StatusConflict, "xray protocol is not enabled on this server")
			return
		}
		config = a.xray.ClientLink(*d)
	default:
		config = a.wg.ClientConfig(*d)
	}
	a.log.Info("join code redeemed", "code", jc.Code, "device_id", d.ID)
	writeJSON(w, http.StatusOK, joinResult{
		DeviceID: d.ID,
		Name:     d.Name,
		Protocol: d.Protocol,
		Config:   config,
	})
}

/* ------------------------- join landing page ------------------------- */

// joinPage is the tiny public page behind https://<domain>/join/<code>. It
// explains what to do with the code and offers the fibr:// deep link; it
// never consumes the code, so previews and accidental opens are harmless.
var joinPage = template.Must(template.New("join").Parse(`<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Fibr — {{.Title}}</title>
<style>
body{font-family:system-ui,-apple-system,sans-serif;background:#0f1216;color:#e8eaf0;margin:0;
display:flex;min-height:100vh;align-items:center;justify-content:center}
.card{max-width:26rem;margin:1rem;padding:2rem;border-radius:16px;background:#181c24;
border:1px solid #262c38;text-align:center}
.code{font:700 2.4rem/1.2 ui-monospace,monospace;letter-spacing:.18em;color:#7ee0a3;
background:#10141b;border-radius:12px;padding:.8rem;margin:1.2rem 0}
a.btn{display:block;margin-top:1.2rem;padding:.9rem;border-radius:12px;background:#4f7cff;
color:#fff;text-decoration:none;font-weight:600}
p{color:#aab3c2;line-height:1.5}
</style>
</head>
<body>
<div class="card">
<h1>Fibr</h1>
<p>{{.Lead}}</p>
<div class="code">{{.Code}}</div>
<p>{{.Hint}}</p>
<a class="btn" href="{{.DeepLink}}">{{.Button}}</a>
</div>
</body>
</html>`))

// handleJoinLanding serves GET /join/{code} for both languages (ru by
// default, en for non-Cyrillic Accept-Language). The page shows the code
// and a fibr://join/<base64(url)> button that opens the installed client.
func (a *API) handleJoinLanding(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	link := JoinLink(a.cfg.Domain, code)
	ru := true
	if al := r.Header.Get("Accept-Language"); len(al) > 0 && al[0] != 'r' && !strings.HasPrefix(al, "ru") {
		ru = strings.Contains(al, "ru") // keep ru when mentioned anywhere
	}
	data := struct {
		Lang, Title, Lead, Hint, Button, Code, DeepLink string
	}{Code: code}
	if ru {
		data.Lang, data.Title = "ru", "Быстрое подключение"
		data.Lead = "Введите этот код в приложении Fibr: «Добавить тоннель» → «По коду»"
		data.Hint = "Код одноразовый и действует ограниченное время."
		data.Button = "Открыть в приложении"
	} else {
		data.Lang, data.Title = "en", "Quick connect"
		data.Lead = "Enter this code in the Fibr app: “Add tunnel” → “By code”"
		data.Hint = "The code is one-time and short-lived."
		data.Button = "Open in the app"
	}
	// The deep link carries the full join URL: the app learns both the server
	// host and the code from one payload (see deep_link_service.dart).
	data.DeepLink = "fibr://join/" + base64.RawURLEncoding.EncodeToString([]byte(link))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := joinPage.Execute(w, data); err != nil {
		a.log.Error("join page render failed", "error", err)
	}
}
