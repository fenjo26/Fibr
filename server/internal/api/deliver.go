package api

// Config-file delivery through Telegram: the "file" path to connect.
//
// Two entry points share it:
//   - the bot command /new [name] (bot package calls IssueDeviceFile): a new
//     WireGuard device is provisioned for the account and its .conf comes
//     back as a document in the chat — "one more file for the next iPhone";
//   - POST /api/me/devices/{id}/send from the Mini App / cabinet: an
//     existing device's .conf is sent to the owner's Telegram chat.
//
// A file is a static key exactly like the QR or the link: every file is its
// own device (own key pair, counted against the account's device cap) and
// can be revoked one by one. WireGuard allows one live endpoint per key, so
// the same file on two phones at once keeps kicking both off.

import (
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"

	"github.com/fenjo26/Fibr/internal/store"
)

// FileSender delivers a document to a Telegram chat (wired to the bot).
type FileSender func(chatID int64, filename string, data []byte, caption string) error

// ErrDeviceLimit is returned by IssueDeviceFile when the account is full.
var ErrDeviceLimit = errors.New("device limit reached")

// ErrXrayDisabled: VLESS was explicitly turned off on this server.
var ErrXrayDisabled = errors.New("xray protocol is disabled on this server")

// SetFileSender wires the Telegram document sender (main.go, bot enabled).
func (a *API) SetFileSender(f FileSender) { a.fileSender = f }

// ConfFileName is the download name of a device config. WireGuard derives
// the tunnel name from it and Android's WireGuard rejects names longer than
// 15 chars or outside [a-zA-Z0-9_=+.-], so the stem is clamped to that.
func ConfFileName(deviceName, ext string) string {
	stem := "fibr-" + fileSafe(deviceName)
	if len(stem) > 15 {
		stem = stem[:15]
	}
	stem = strings.TrimRight(stem, "-")
	if stem == "fibr" {
		// Nothing latin survived (e.g. a Cyrillic name): a short stable
		// hash keeps different devices from importing as the same tunnel.
		h := fnv.New32a()
		h.Write([]byte(deviceName))
		stem = fmt.Sprintf("fibr-%08x", h.Sum32())
	}
	return stem + "." + ext
}

// DeviceSlots reports how many devices the account holds and may hold.
func (a *API) DeviceSlots(userID int64) (used, limit int, err error) {
	u, err := a.store.GetUser(userID)
	if err != nil {
		return 0, 0, err
	}
	devices, err := a.store.ListDevices(userID)
	if err != nil {
		return 0, 0, err
	}
	limit, err = a.effectiveMaxDevices(u)
	return len(devices), limit, err
}

// ErrAWGDisabled: the AmneziaWG tunnel is not running on this server.
var ErrAWGDisabled = errors.New("amneziawg protocol is disabled on this server")

// IssueDeviceFile provisions a new WireGuard device for the account and
// returns its config file. ErrDeviceLimit when the account is full.
func (a *API) IssueDeviceFile(userID int64, name string) (filename string, data []byte, err error) {
	return a.IssueDeviceFileProto(userID, name, store.ProtocolWG)
}

// IssueDeviceFileProto is IssueDeviceFile for "wg" or "awg" (AmneziaWG:
// the file opens in AmneziaVPN, the bot's /amnezia).
func (a *API) IssueDeviceFileProto(userID int64, name, protocol string) (filename string, data []byte, err error) {
	if protocol == store.ProtocolAWG && a.awgm == nil {
		return "", nil, ErrAWGDisabled
	}
	used, limit, err := a.DeviceSlots(userID)
	if err != nil {
		return "", nil, err
	}
	if used >= limit {
		return "", nil, ErrDeviceLimit
	}
	d, err := a.provisionDevice(userID, sanitizeName(name, fmt.Sprintf("device-%d", used+1)), protocol)
	if err != nil {
		return "", nil, err
	}
	content, _, err := a.renderCredential(*d)
	if err != nil {
		return "", nil, err
	}
	a.touchConfigFetched(d.ID)
	return ConfFileName(d.Name, "conf"), []byte(content), nil
}

// IssueVlessLink provisions a new VLESS (xray) device and returns its
// vless:// link — the bot's /vless command.
func (a *API) IssueVlessLink(userID int64, name string) (string, error) {
	if !a.cfg.XrayEnabled || a.xray == nil {
		return "", ErrXrayDisabled
	}
	used, limit, err := a.DeviceSlots(userID)
	if err != nil {
		return "", err
	}
	if used >= limit {
		return "", ErrDeviceLimit
	}
	d, err := a.provisionDevice(userID, sanitizeName(name, fmt.Sprintf("vless-%d", used+1)), store.ProtocolXray)
	if err != nil {
		return "", err
	}
	link, _, err := a.renderCredential(*d)
	if err != nil {
		return "", err
	}
	a.touchConfigFetched(d.ID)
	return link, nil
}

// handleMeSendDevice sends an owned device's config to the caller's
// Telegram chat as a file (Mini App "Send file to Telegram").
func (a *API) handleMeSendDevice(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	d, ok := a.ownedDevice(w, r, id.userID)
	if !ok {
		return
	}
	if a.fileSender == nil {
		a.errJSON(w, http.StatusServiceUnavailable, "telegram bot is not configured")
		return
	}
	u, err := a.store.GetUser(id.userID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	if u.TelegramID == nil {
		a.errJSON(w, http.StatusConflict, "account has no telegram chat")
		return
	}
	content, code, err := a.renderCredential(*d)
	if err != nil {
		a.errJSON(w, code, err.Error())
		return
	}
	ext := "conf"
	caption := deviceFileCaption
	if d.Protocol == store.ProtocolXray {
		ext = "txt"
		caption = "Fibr: VLESS link for «" + d.Name + "». Open the file, copy the link and paste it into v2rayNG / Hiddify / Streisand / AmneziaVPN."
	}
	if err := a.fileSender(*u.TelegramID, ConfFileName(d.Name, ext), []byte(content), caption); err != nil {
		a.log.Warn("send config to telegram failed", "device_id", d.ID, "error", err)
		// The usual cause: the user never pressed Start in the bot chat.
		a.errJSON(w, http.StatusBadGateway, "cannot send: open the bot and press Start first")
		return
	}
	a.touchConfigFetched(d.ID)
	writeJSON(w, http.StatusOK, map[string]any{"sent": true})
}

// deviceFileCaption goes under a .conf sent from the Mini App (the bot's
// /new command has its own localized caption).
const deviceFileCaption = "Fibr config: tap the file → open with WireGuard or AmneziaVPN (or Fibr). One file = one device."
