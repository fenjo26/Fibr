package api

// TV pairing ("connect the TV from the phone"): typing links or codes with a
// remote is miserable, so the TV asks the server for a short pairing code
// and shows it with a QR; the phone (already signed in via Telegram) scans
// it, picks or creates a device, and the TV picks the config up by polling.
//
//	TV:    POST /api/pair                   -> {code, secret, link, qr, expires_at}
//	TV:    GET  /api/pair/{code}?secret=…   -> {status: waiting|ready, config}
//	TV:    GET  /api/pair/{code}/qr.png     -> QR of the link (no QR lib on the TV)
//	phone: POST /api/me/pair/{code}         {device_id?, name?} (Mini App)
//
// The QR link is t.me/<bot>?start=tv_CODE (the bot answers with the Mini
// App button carrying ?pair=CODE) or, without a bot, /miniapp/?pair=CODE.
// Pairings live in memory for pairTTL: a restart just means "show a new QR".
// The code alone is not a secret worth stealing — knowing it only lets
// someone GIVE the TV a device; the config goes out only with the TV's
// secret, and only once.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/fenjo26/Fibr/internal/store"
)

const (
	pairTTL      = 10 * time.Minute
	pairCodeLen  = 6
	pairAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ" // no 0/O, 1/I/L: read off a TV
	maxPairings  = 1000
)

type pairing struct {
	secret    string
	expires   time.Time
	config    string // set by the phone; handed to the TV once
	deviceID  int64
	delivered bool
}

type pairStore struct {
	mu sync.Mutex
	m  map[string]*pairing
}

func (p *pairStore) gc(now time.Time) {
	for code, pr := range p.m {
		if now.After(pr.expires) || pr.delivered {
			delete(p.m, code)
		}
	}
}

func genPairCode() (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(pairAlphabet)))
	for i := 0; i < pairCodeLen; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(pairAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// normPairCode upper-cases and strips what people type around a code
// ("tv_", spaces, dashes).
func normPairCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "TV_")
	return strings.NewReplacer(" ", "", "-", "").Replace(s)
}

// PairLink is what the TV's QR encodes.
func PairLink(domain, botUsername, code string) string {
	if b := strings.TrimPrefix(strings.TrimSpace(botUsername), "@"); b != "" {
		return "https://t.me/" + b + "?start=tv_" + code
	}
	d := strings.TrimSuffix(strings.TrimSpace(domain), "/")
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if d == "" {
		return "/miniapp/?pair=" + code
	}
	return "https://" + d + "/miniapp/?pair=" + code
}

// handlePairCreate (TV, public): a fresh pairing code.
func (a *API) handlePairCreate(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	sec := make([]byte, 16)
	if _, err := rand.Read(sec); err != nil {
		a.errJSON(w, http.StatusInternalServerError, "rng failure")
		return
	}
	a.pairs.mu.Lock()
	defer a.pairs.mu.Unlock()
	if a.pairs.m == nil {
		a.pairs.m = map[string]*pairing{}
	}
	a.pairs.gc(now)
	if len(a.pairs.m) >= maxPairings {
		a.errJSON(w, http.StatusTooManyRequests, "too many pending pairings, retry later")
		return
	}
	var code string
	for attempt := 0; ; attempt++ {
		c, err := genPairCode()
		if err != nil || attempt > 10 {
			a.errJSON(w, http.StatusInternalServerError, "cannot generate code")
			return
		}
		if _, taken := a.pairs.m[c]; !taken {
			code = c
			break
		}
	}
	pr := &pairing{secret: hex.EncodeToString(sec), expires: now.Add(pairTTL)}
	a.pairs.m[code] = pr
	writeJSON(w, http.StatusCreated, map[string]any{
		"code":       code,
		"secret":     pr.secret,
		"link":       PairLink(a.cfg.Domain, a.botName(), code),
		"qr":         "/api/pair/" + code + "/qr.png",
		"expires_at": pr.expires.UTC().Format(time.RFC3339),
	})
}

// lookupPair returns a live pairing (nil when unknown/expired).
func (a *API) lookupPair(code string, now time.Time) *pairing {
	if a.pairs.m == nil {
		return nil
	}
	pr := a.pairs.m[normPairCode(code)]
	if pr == nil || now.After(pr.expires) || pr.delivered {
		return nil
	}
	return pr
}

// handlePairPoll (TV, public + secret): waiting until the phone attaches a
// device, then the config — exactly once.
func (a *API) handlePairPoll(w http.ResponseWriter, r *http.Request) {
	a.pairs.mu.Lock()
	defer a.pairs.mu.Unlock()
	pr := a.lookupPair(r.PathValue("code"), time.Now())
	if pr == nil || r.URL.Query().Get("secret") != pr.secret {
		a.errJSON(w, http.StatusNotFound, "pairing not found or expired")
		return
	}
	if pr.config == "" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "waiting"})
		return
	}
	pr.delivered = true
	a.touchConfigFetched(pr.deviceID)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "config": pr.config})
}

// handlePairQR (TV, public): the QR of the pairing link, so the TV client
// needs no QR library.
func (a *API) handlePairQR(w http.ResponseWriter, r *http.Request) {
	code := normPairCode(r.PathValue("code"))
	a.pairs.mu.Lock()
	pr := a.lookupPair(code, time.Now())
	a.pairs.mu.Unlock()
	if pr == nil {
		a.errJSON(w, http.StatusNotFound, "pairing not found or expired")
		return
	}
	png, err := qrcode.Encode(PairLink(a.cfg.Domain, a.botName(), code), qrcode.Medium, 512)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "qr generation failed")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

// handleMePair (phone, signed in): attach an existing WireGuard device of
// the caller, or a new one, to the TV's pairing code.
func (a *API) handleMePair(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	var body struct {
		DeviceID int64  `json:"device_id"`
		Name     string `json:"name"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := a.store.GetUser(id.userID)
	if err != nil {
		a.errJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if a.userExpired(u) {
		a.errJSON(w, http.StatusForbidden, msgSubscriptionExpired)
		return
	}
	code := normPairCode(r.PathValue("code"))
	a.pairs.mu.Lock()
	pr := a.lookupPair(code, time.Now())
	busy := pr != nil && pr.config != ""
	a.pairs.mu.Unlock()
	if pr == nil {
		a.errJSON(w, http.StatusNotFound, "code not found or expired — show a new QR on the TV")
		return
	}
	if busy {
		a.errJSON(w, http.StatusConflict, "this TV is already being connected")
		return
	}

	var d *store.Device
	if body.DeviceID != 0 {
		d, err = a.store.GetDevice(body.DeviceID)
		if err != nil || d.UserID != id.userID {
			a.errJSON(w, http.StatusNotFound, "device not found")
			return
		}
		if d.Protocol == store.ProtocolXray {
			a.errJSON(w, http.StatusBadRequest, "the TV app speaks WireGuard: pick a WG device")
			return
		}
	} else {
		used, limit, err := a.DeviceSlots(id.userID)
		if err != nil {
			a.errJSON(w, http.StatusInternalServerError, "storage error")
			return
		}
		if used >= limit {
			a.errJSON(w, http.StatusConflict, "device limit reached")
			return
		}
		d, err = a.provisionDevice(id.userID, sanitizeName(body.Name, fmt.Sprintf("TV-%d", used+1)), store.ProtocolWG)
		if err != nil {
			a.errJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	content, code2, err := a.renderCredential(*d)
	if err != nil {
		a.errJSON(w, code2, err.Error())
		return
	}

	a.pairs.mu.Lock()
	defer a.pairs.mu.Unlock()
	pr = a.lookupPair(code, time.Now())
	if pr == nil || pr.config != "" {
		a.errJSON(w, http.StatusConflict, "this TV is already being connected")
		return
	}
	pr.config, pr.deviceID = content, d.ID
	a.log.Info("tv paired", "user_id", id.userID, "device_id", d.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "device": newDeviceOut(*d, "")})
}
