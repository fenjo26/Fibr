package api

// Protected DNS and the game catalog (docs/dns-games.md §3): public status
// probes for the clients, admin stats/log/blocklist endpoints and the
// per-device profile switch for the Mini App / cabinet / admin panel.
//
// The public endpoints answer (with "enabled":false / empty catalog) when
// the resolver or the catalog is off, so clients can degrade gracefully
// instead of guessing at 404s.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fenjo26/Fibr/internal/dns"
	"github.com/fenjo26/Fibr/internal/store"
	"github.com/skip2/go-qrcode"
)

// handleDNSStatus is the public probe the app's DNS-protection card polls
// through the tunnel gateway (before login, like /api/health).
func (a *API) handleDNSStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":         a.dnssrv != nil,
		"default_profile": a.cfg.DNSDefaultProfile,
	})
}

// handleDNSStats serves the 24h aggregate for the admin DNS page.
func (a *API) handleDNSStats(w http.ResponseWriter, r *http.Request) {
	if a.dnssrv == nil {
		a.errJSON(w, http.StatusServiceUnavailable, "dns protection is disabled on this server")
		return
	}
	stats, err := a.dnssrv.Stats24h()
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleDNSQueryLog serves recent queries for the admin DNS page; an
// optional ?device=<tunnel ip> filters one device, ?limit= caps the rows.
func (a *API) handleDNSQueryLog(w http.ResponseWriter, r *http.Request) {
	if a.dnssrv == nil {
		a.errJSON(w, http.StatusServiceUnavailable, "dns protection is disabled on this server")
		return
	}
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	device := strings.TrimSpace(r.URL.Query().Get("device"))
	entries, err := a.dnssrv.QueryLogEntries(limit, device)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleDNSLists lists the blocklists (builtin + custom).
func (a *API) handleDNSLists(w http.ResponseWriter, r *http.Request) {
	if a.dnssrv == nil {
		a.errJSON(w, http.StatusServiceUnavailable, "dns protection is disabled on this server")
		return
	}
	writeJSON(w, http.StatusOK, a.dnssrv.Lists())
}

// handleDNSAddList adds a custom blocklist URL (AdGuard-style hosts/adblock
// format; docs/dns-games.md §1 "Lists").
func (a *API) handleDNSAddList(w http.ResponseWriter, r *http.Request) {
	if a.dnssrv == nil {
		a.errJSON(w, http.StatusServiceUnavailable, "dns protection is disabled on this server")
		return
	}
	var body struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.URL = strings.TrimSpace(body.URL)
	if body.Name == "" || body.URL == "" {
		a.errJSON(w, http.StatusBadRequest, "name and url are required")
		return
	}
	if !strings.HasPrefix(body.URL, "https://") && !strings.HasPrefix(body.URL, "http://") {
		a.errJSON(w, http.StatusBadRequest, "url must start with http:// or https://")
		return
	}
	if err := a.dnssrv.AddList(body.Name, body.URL); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"added": body.Name})
}

// handleDNSSetList toggles a list (builtin lists may be disabled but not
// renamed or deleted).
func (a *API) handleDNSSetList(w http.ResponseWriter, r *http.Request) {
	if a.dnssrv == nil {
		a.errJSON(w, http.StatusServiceUnavailable, "dns protection is disabled on this server")
		return
	}
	name := r.PathValue("name")
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Enabled == nil {
		a.errJSON(w, http.StatusBadRequest, "enabled is required")
		return
	}
	if err := a.dnssrv.SetListEnabled(name, *body.Enabled); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "enabled": *body.Enabled})
}

// handleDNSRemoveList deletes a custom list (builtin lists are refused by
// the list manager).
func (a *API) handleDNSRemoveList(w http.ResponseWriter, r *http.Request) {
	if a.dnssrv == nil {
		a.errJSON(w, http.StatusServiceUnavailable, "dns protection is disabled on this server")
		return
	}
	if err := a.dnssrv.RemoveList(r.PathValue("name")); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDNSRefreshLists re-downloads every enabled list now (the admin
// "Refresh" button); the 24h loop keeps running regardless.
func (a *API) handleDNSRefreshLists(w http.ResponseWriter, r *http.Request) {
	if a.dnssrv == nil {
		a.errJSON(w, http.StatusServiceUnavailable, "dns protection is disabled on this server")
		return
	}
	if err := a.dnssrv.RefreshAllLists(r.Context()); err != nil {
		a.errJSON(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refreshed": true, "sets": a.dnssrv.SetsLen()})
}

// parseAllowJSON decodes a stored allowlist JSON array, nil on anything odd.
func parseAllowJSON(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(s), &out) != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validateDomain accepts plain lowercase hostnames ("example.com") — the
// personal allowlist's only shape.
func validateDomain(s string) error {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 {
		return errors.New("empty or too long domain")
	}
	if strings.Contains(s, "://") || strings.ContainsAny(s, "/ \\ \"'<>@#?&=") {
		return errors.New("a bare domain is expected, without http:// or a path")
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return errors.New("a full domain is expected, e.g. example.com")
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 {
			return errors.New("malformed domain label")
		}
		for _, c := range l {
			if !(c == '-' || c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
				return errors.New("only letters, digits, dashes and dots are allowed")
			}
		}
	}
	return nil
}

// normalizeAllow validates and canonicalises the owner's never-block list.
// Max 64 entries: the resolver walks it on every query.
func normalizeAllow(in []string) (string, error) {
	if len(in) > 64 {
		return "", errors.New("at most 64 personal exceptions")
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		d := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), ".")))
		if d == "" {
			continue
		}
		if err := validateDomain(d); err != nil {
			return "", fmt.Errorf("%q: %w", raw, err)
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// handleMeDeviceDNS is the owner's per-device filter switch (Mini App /
// cabinet): {"profile":"off|adblock|family","schedule":{...}|null,
// "allow":["example.com",...]}. The allowlist is the user's own exception
// set — it overrides every admin list for this device. A change re-renders
// the device config on the next download (DNS = <gateway> while the
// profile is not off), so the response tells the client to re-fetch.
func (a *API) handleMeDeviceDNS(w http.ResponseWriter, r *http.Request) {
	id := ident(r)
	d, ok := a.ownedDevice(w, r, id.userID)
	if !ok {
		return
	}
	if a.dnssrv == nil {
		a.errJSON(w, http.StatusServiceUnavailable, "dns protection is disabled on this server")
		return
	}
	if d.Protocol == store.ProtocolXray {
		a.errJSON(w, http.StatusBadRequest, "dns profiles apply to wireguard devices only")
		return
	}
	var body struct {
		Profile  string        `json:"profile"`
		Schedule *dns.Schedule `json:"schedule"`
		Allow    *[]string     `json:"allow"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Profile == "" {
		body.Profile = a.cfg.DNSDefaultProfile
	}
	schedJSON := ""
	if body.Profile == string(dns.ProfileFamily) && body.Schedule != nil {
		if err := validateSchedule(*body.Schedule); err != nil {
			a.errJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		b, err := json.Marshal(*body.Schedule)
		if err != nil {
			a.errJSON(w, http.StatusInternalServerError, "cannot store schedule")
			return
		}
		schedJSON = string(b)
	}
	// nil Allow = keep the stored exceptions; [] clears them.
	var allowPtr *string
	if body.Allow != nil {
		allowJSON, err := normalizeAllow(*body.Allow)
		if err != nil {
			a.errJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		allowPtr = &allowJSON
	}
	if err := a.setDeviceDNS(d.ID, body.Profile, schedJSON, allowPtr); err != nil {
		a.errJSON(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := a.store.GetDevice(d.ID)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "storage error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"device":       newDeviceOut(*d, ""),
		"config_stale": true, // DNS line changed — re-download the config
	})
}

// setDeviceDNS validates the profile and persists it. profile may be ""
// (back to the server default); schedule is pre-validated JSON or "";
// allowlist nil = leave the stored one untouched.
func (a *API) setDeviceDNS(deviceID int64, profile, scheduleJSON string, allowlist *string) error {
	if a.dnssrv == nil {
		return errors.New("dns protection is disabled on this server")
	}
	switch profile {
	case "", string(dns.ProfileOff), string(dns.ProfileAdblock), string(dns.ProfileFamily):
	default:
		return errors.New("profile must be one of off, adblock, family")
	}
	if scheduleJSON != "" {
		var s dns.Schedule
		if err := json.Unmarshal([]byte(scheduleJSON), &s); err != nil {
			return errors.New("schedule must be valid JSON")
		}
		if err := validateSchedule(s); err != nil {
			return err
		}
	}
	return a.store.SetDeviceDNS(deviceID, profile, scheduleJSON, allowlist)
}

// validateSchedule checks the parental window fields the UI can send; the
// resolver itself fails soft (an unparsable window never bricks DNS).
func validateSchedule(s dns.Schedule) error {
	for _, d := range s.Days {
		if d < 1 || d > 7 {
			return errors.New("schedule days must be 1 (Mon) .. 7 (Sun)")
		}
	}
	for _, hm := range []string{s.Start, s.End} {
		if hm == "" {
			continue // an incomplete window is "no schedule"
		}
		if _, ok := parseHHMMPublic(hm); !ok {
			return errors.New("schedule start/end must be HH:MM (00:00..23:59)")
		}
	}
	if s.TZ != "" {
		if _, err := time.LoadLocation(s.TZ); err != nil {
			return errors.New("schedule tz is not a valid IANA zone (e.g. Europe/Berlin)")
		}
	}
	return nil
}

// parseHHMMPublic mirrors dns.parseHHMM for validation (unexported there).
func parseHHMMPublic(s string) (int, bool) {
	hh, mm, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, false
	}
	h, e1 := strconv.Atoi(hh)
	m, e2 := strconv.Atoi(mm)
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// handleGames serves the public game catalog (the app's game mode screen).
func (a *API) handleGames(w http.ResponseWriter, r *http.Request) {
	if a.gamesCat == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, a.gamesCat.List())
}

// handleGameRoutes serves the CIDR ranges for the selected games — the
// AllowedIPs of the app's gaming tunnel. Unknown ids are silently skipped.
func (a *API) handleGameRoutes(w http.ResponseWriter, r *http.Request) {
	if a.gamesCat == nil {
		writeJSON(w, http.StatusOK, map[string][]string{})
		return
	}
	ids := strings.Split(r.URL.Query().Get("ids"), ",")
	writeJSON(w, http.StatusOK, a.gamesCat.Routes(ids))
}

// gameRoutesFor resolves ?games= to the deduplicated CIDR list of the
// selected games. ok is false when the catalog is off or nothing matched.
func (a *API) gameRoutesFor(raw string) ([]string, bool) {
	if a.gamesCat == nil {
		return nil, false
	}
	ids := strings.Split(raw, ",")
	seen := map[string]bool{}
	var out []string
	for _, game := range a.gamesCat.Routes(ids) {
		for _, cidr := range game {
			if !seen[cidr] {
				seen[cidr] = true
				out = append(out, cidr)
			}
		}
	}
	return out, len(out) > 0
}

// handleMeGameConfig renders the device's GAME-tunnel config (Mini App /
// cabinet game mode): the same peer, AllowedIPs = the selected games' CIDRs
// (docs/dns-games.md §2). The client imports it as a second tunnel.
func (a *API) handleMeGameConfig(w http.ResponseWriter, r *http.Request) {
	content, name, code, err := a.renderGameCredential(w, r)
	if err != nil {
		a.errJSON(w, code, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Write([]byte(content))
}

// handleMeGameQR is the QR-code twin of the game config endpoint.
func (a *API) handleMeGameQR(w http.ResponseWriter, r *http.Request) {
	content, _, code, err := a.renderGameCredential(w, r)
	if err != nil {
		a.errJSON(w, code, err.Error())
		return
	}
	png, err := qrcode.Encode(content, qrcode.Medium, 512)
	if err != nil {
		a.errJSON(w, http.StatusInternalServerError, "qr generation failed")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(png)
}

// renderGameCredential shares the validation between the config and QR
// handlers. The returned code is 0 on success.
func (a *API) renderGameCredential(w http.ResponseWriter, r *http.Request) (content, filename string, code int, err error) {
	id := ident(r)
	d, ok := a.ownedDevice(w, r, id.userID)
	if !ok {
		return "", "", http.StatusNotFound, errors.New("device not found")
	}
	if d.Protocol == store.ProtocolXray {
		return "", "", http.StatusBadRequest, errors.New("game mode applies to wireguard devices only")
	}
	cidrs, ok := a.gameRoutesFor(r.URL.Query().Get("games"))
	if !ok {
		return "", "", http.StatusBadRequest, errors.New("no games selected or the catalog is empty")
	}
	ep, _, err := a.resolveActiveEndpoint()
	if err != nil {
		return "", "", http.StatusInternalServerError, errors.New("cannot resolve active endpoint")
	}
	m := a.wg
	if d.Protocol == store.ProtocolAWG {
		if a.awgm == nil {
			return "", "", http.StatusConflict, errors.New("amneziawg protocol is not enabled on this server")
		}
		m = a.awgm
	}
	a.touchConfigFetched(d.ID)
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "wiresock" || format == "pc" || format == "tunnlto" {
		var apps []string
		if a.gamesCat != nil {
			apps = a.gamesCat.Processes(strings.Split(r.URL.Query().Get("games"), ","))
		}
		return m.ClientConfigGameWireSockAt(*d, ep, apps, cidrs), ConfFileName(d.Name+"-wiresock-game", "conf"), 0, nil
	}
	return m.ClientConfigGameAt(*d, ep, cidrs), ConfFileName(d.Name+"-game", "conf"), 0, nil
}

// GameHubDiagnostic represents latency from this Fibr server to a regional gaming hub.
type GameHubDiagnostic struct {
	ID     string `json:"id"`
	City   string `json:"city"`
	Region string `json:"region"`
	RTTMs  int    `json:"rtt_ms"`
	Status string `json:"status"` // "excellent", "good", "fair", "unreachable"
}

// DiagnosticsResponse is the payload for GET /api/games/diagnostics.
type DiagnosticsResponse struct {
	Timestamp time.Time           `json:"timestamp"`
	Hubs      []GameHubDiagnostic `json:"hubs"`
}

var (
	diagMu    sync.Mutex
	diagCache *DiagnosticsResponse
	diagTime  time.Time
)

// handleGameDiagnostics measures and returns server-to-hub network latency.
func (a *API) handleGameDiagnostics(w http.ResponseWriter, r *http.Request) {
	diagMu.Lock()
	defer diagMu.Unlock()

	if diagCache != nil && time.Since(diagTime) < 30*time.Second {
		writeJSON(w, http.StatusOK, diagCache)
		return
	}

	targets := []struct {
		id, city, region, addr string
	}{
		{"fra", "Frankfurt", "EU West", "162.249.72.1:27015"},
		{"sto", "Stockholm", "EU North", "155.133.252.1:27015"},
		{"ams", "Amsterdam", "EU Central", "185.60.112.157:1119"},
		{"waw", "Warsaw", "EU East", "155.133.230.1:27015"},
	}

	hubs := make([]GameHubDiagnostic, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(idx int, id, city, region, addr string) {
			defer wg.Done()
			start := time.Now()
			conn, err := net.DialTimeout("tcp", addr, 350*time.Millisecond)
			dur := time.Since(start)
			rtt := int(dur / time.Millisecond)
			if conn != nil {
				conn.Close()
			}
			reachable := err == nil || strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "reset")
			status := "unreachable"
			if reachable {
				if rtt <= 25 {
					status = "excellent"
				} else if rtt <= 60 {
					status = "good"
				} else {
					status = "fair"
				}
			} else {
				rtt = -1
			}
			hubs[idx] = GameHubDiagnostic{
				ID:     id,
				City:   city,
				Region: region,
				RTTMs:  rtt,
				Status: status,
			}
		}(i, t.id, t.city, t.region, t.addr)
	}
	wg.Wait()

	resp := &DiagnosticsResponse{
		Timestamp: time.Now().UTC(),
		Hubs:      hubs,
	}
	diagCache = resp
	diagTime = time.Now()

	writeJSON(w, http.StatusOK, resp)
}
