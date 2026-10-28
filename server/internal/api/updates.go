package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

/* One-click updates via the opt-in updater sidecar (docs/updates.md).
 *
 * The updater (server/cmd/fibr-updater) is a tiny HTTP agent that runs in
 * the compose network behind the "updater" profile, answers on :8912 and
 * knows how to `git reset --hard origin/main` the mounted checkout and
 * rebuild the stack through the docker socket. These two endpoints are the
 * admin-panel side: proxy the status, proxy the trigger. When the sidecar
 * is not enabled, status answers {"enabled":false} — a normal state, not an
 * error — and the panel simply hides the update badge. */

// defaultUpdaterTimeout bounds both proxy calls. Status must never hang the
// panel; apply answers 202 from the sidecar long before any real work
// finishes. updaterTimeout is a var so tests can shrink the budget.
var updaterTimeout = defaultUpdaterTimeout

const defaultUpdaterTimeout = 4 * time.Second

// handleAdminUpdates reports the updater's view of the deployment. Response:
// {"enabled":true, ...updater status} when the sidecar answers, otherwise
// {"enabled":false} (sidecar off, unreachable or slow — all the same to the
// panel: no badge).
func (a *API) handleAdminUpdates(w http.ResponseWriter, r *http.Request) {
	status, ok := a.fetchUpdaterStatus()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	status["enabled"] = true
	writeJSON(w, http.StatusOK, status)
}

// fetchUpdaterStatus calls GET <updater>/status and decodes it into a loose
// map (the panel reads the fields defensively anyway). ok=false covers every
// failure mode: empty URL, transport error, non-200, garbage body.
func (a *API) fetchUpdaterStatus() (map[string]any, bool) {
	if a.updaterURL == "" {
		return nil, false
	}
	client := &http.Client{Timeout: updaterTimeout}
	resp, err := client.Get(a.updaterURL + "/status")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	var status map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&status); err != nil {
		return nil, false
	}
	return status, true
}

// handleAdminUpdatesApply triggers the rebuild. The sidecar answers 202 as
// soon as the work is queued — the fibr container itself is recreated a
// minute later, which drops THIS connection; the panel expects that and
// polls /api/health until the API is back (docs/updates.md).
func (a *API) handleAdminUpdatesApply(w http.ResponseWriter, r *http.Request) {
	if a.updaterURL == "" {
		a.errJSON(w, http.StatusServiceUnavailable, "updater not enabled")
		return
	}
	client := &http.Client{Timeout: updaterTimeout}
	resp, err := client.Post(a.updaterURL+"/update", "application/json", nil)
	if err != nil {
		a.errJSON(w, http.StatusServiceUnavailable, "updater not enabled")
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16<<10))
	switch resp.StatusCode {
	case http.StatusAccepted:
		a.log.Info("stack update triggered", "by", ident(r).userID)
		writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
	case http.StatusConflict:
		// An update is already running — pass the condition through.
		a.errJSON(w, http.StatusConflict, "update already in progress")
	default:
		a.errJSON(w, http.StatusBadGateway, "updater error")
	}
}
