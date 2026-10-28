// Command fibr-updater is the opt-in one-click update sidecar (docs/updates.md).
//
// It runs inside the compose network (profile "updater", no published ports)
// with the Fibr checkout mounted at FIBR_REPO_DIR (default /repo) and the
// docker socket mounted so it can rebuild the stack:
//
//	GET  /status → current/remote commit, behind flag, latest release tag
//	POST /update → 202 immediately, then git fetch + reset + compose up --build
//
// Nothing here is automatic: no timers, no webhooks — the superadmin presses
// the button in the admin panel, fibr proxies to this agent. The fibr API
// talks to it at FIBR_UPDATER_URL (default http://updater:8912).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultAddr    = ":8912"
	defaultRepoDir = "/repo"
	// The lockfile keeps a second POST /update from racing the running one.
	defaultLockfile = "/tmp/fibr-updating"
	defaultRepo     = "fenjo26/Fibr"
	defaultBranch   = "main"

	// git queries are quick; fetch/reset/compose are not, so they get their
	// own generous budgets (builds can take minutes on a small VPS).
	gitTimeout     = 10 * time.Second
	fetchTimeout   = 2 * time.Minute
	composeTimeout = 20 * time.Minute
	releaseTimeout = 5 * time.Second
)

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	s := &server{
		addr:       envOr("FIBR_UPDATER_ADDR", defaultAddr),
		repoDir:    envOr("FIBR_REPO_DIR", defaultRepoDir),
		lockfile:   envOr("FIBR_UPDATER_LOCKFILE", defaultLockfile),
		githubRepo: envOr("FIBR_UPDATER_REPO", defaultRepo),
		branch:     envOr("FIBR_UPDATER_BRANCH", defaultBranch),
		log:        log,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("POST /update", s.handleUpdate)
	log.Info("fibr-updater listening", "addr", s.addr, "repo", s.repoDir, "branch", s.branch)
	if err := http.ListenAndServe(s.addr, mux); err != nil {
		log.Error("listen failed", "error", err)
		os.Exit(1)
	}
}

type server struct {
	addr       string
	repoDir    string
	lockfile   string
	githubRepo string
	branch     string
	log        *slog.Logger
}

/* ------------------------- status ------------------------- */

// statusResponse mirrors what the admin panel needs to decide whether to show
// the "update available" badge. Any git problem becomes an "error" string,
// never a 500 — a broken checkout is a state, not a crash.
type statusResponse struct {
	Current       string `json:"current"`        // git describe --tags --always --dirty
	CurrentSHA    string `json:"current_sha"`    // git rev-parse HEAD
	RemoteSHA     string `json:"remote_sha"`     // git ls-remote origin HEAD
	Behind        bool   `json:"behind"`         // current_sha != remote_sha
	LatestRelease string `json:"latest_release"` // tag from GitHub releases/latest
	Updating      bool   `json:"updating"`       // a POST /update is in flight
	Error         string `json:"error,omitempty"`
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{Updating: s.updating()}

	describe, err := runCmd(gitTimeout, s.repoDir, "git", "describe", "--tags", "--always", "--dirty")
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Current = firstLine(describe)
	}
	if sha, err := runCmd(gitTimeout, s.repoDir, "git", "rev-parse", "HEAD"); err == nil {
		resp.CurrentSHA = firstLine(sha)
	}
	// ls-remote takes a remote name (resolved inside repoDir) or a URL;
	// "origin" is exactly what we want.
	if out, err := runCmd(gitTimeout, s.repoDir, "git", "ls-remote", "origin", "HEAD"); err == nil {
		resp.RemoteSHA = parseLSRemote(out)
	} else if resp.Error == "" {
		resp.Error = err.Error()
	}
	resp.Behind = resp.CurrentSHA != "" && resp.RemoteSHA != "" && resp.CurrentSHA != resp.RemoteSHA

	resp.LatestRelease = fetchLatestRelease(r.Context(), s.githubRepo)
	writeJSON(w, http.StatusOK, resp)
}

// fetchLatestRelease asks the GitHub API for the newest release tag. Best
// effort: any failure (offline, rate limit, no releases yet) yields "" — the
// badge logic then relies on the commit comparison alone.
func fetchLatestRelease(ctx context.Context, repo string) string {
	if repo == "" {
		return ""
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	cctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return ""
	}
	return parseReleaseTag(body)
}

/* ------------------------- update ------------------------- */

// handleUpdate kicks the rebuild off in the background and answers 202 right
// away — the compose step takes minutes, far beyond any sane HTTP timeout.
// A second click while one runs gets 409 (lockfile still present).
func (s *server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	lock, err := s.tryLock()
	if err != nil {
		if os.IsExist(err) {
			writeJSON(w, http.StatusConflict, map[string]any{"started": false, "error": "update already in progress"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"started": false, "error": "cannot lock: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
	go func() {
		defer lock.Close()
		defer os.Remove(s.lockfile)
		s.runUpdate()
	}()
}

// staleLock is how old the lockfile may get before a killed run (OOM,
// container restart) is assumed and the lock is reclaimed. Compose builds
// take minutes, never half an hour.
const staleLock = 30 * time.Minute

// tryLock creates the lockfile exclusively. A leftover lock older than
// staleLock is removed first so one crash cannot 409 updates forever.
func (s *server) tryLock() (*os.File, error) {
	if info, err := os.Stat(s.lockfile); err == nil && time.Since(info.ModTime()) > staleLock {
		_ = os.Remove(s.lockfile)
	}
	return os.OpenFile(s.lockfile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

func (s *server) updating() bool {
	info, err := os.Stat(s.lockfile)
	return err == nil && time.Since(info.ModTime()) < staleLock
}

// runUpdate is the whole trick: move the checkout to origin/<branch>, then
// rebuild and restart the stack. Output goes to the container log; there is
// no progress streaming — the panel just polls /api/health until the API is
// back. NB: `up -d --build` also rebuilds THIS container; when the updater
// itself changed, that recreate kills this command mid-flight — everything
// else is already up by then, and the fresh updater container takes over
// (docs/updates.md, "Что происходит под капотом").
func (s *server) runUpdate() {
	if out, err := runCmd(fetchTimeout, s.repoDir, "git", "fetch", "origin"); err != nil {
		s.log.Error("update: git fetch failed", "error", err, "output", firstLine(out))
		return
	}
	if out, err := runCmd(gitTimeout, s.repoDir, "git", "reset", "--hard", "origin/"+s.branch); err != nil {
		s.log.Error("update: git reset failed", "error", err, "output", firstLine(out))
		return
	}
	composeFile := s.repoDir + "/install/docker-compose.yml"
	s.log.Info("update: rebuilding stack", "compose", composeFile)
	if out, err := runCmd(composeTimeout, s.repoDir, "docker", "compose", "-f", composeFile, "up", "-d", "--build"); err != nil {
		s.log.Error("update: compose up failed", "error", err, "output", lastLines(out, 20))
		return
	}
	s.log.Info("update: stack rebuilt")
}

/* ------------------------- exec & parse helpers ------------------------- */

// runCmd runs a command with a timeout and returns its combined output.
// The error message embeds the first line of output so /status stays small.
func runCmd(timeout time.Duration, dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		msg := firstLine(buf.String())
		if msg == "" {
			msg = err.Error()
		}
		return buf.String(), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return buf.String(), nil
}

// firstLine trims command output down to its first line for log/error fields.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// lastLines keeps the tail of (potentially long) compose output.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// parseLSRemote extracts the commit hash from `git ls-remote` output. The
// first line looks like "<sha>\trefs/heads/main" (or "<sha>\tHEAD"); the
// hash is simply its first whitespace-separated field.
func parseLSRemote(out string) string {
	fields := strings.Fields(firstLine(out))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// parseReleaseTag pulls "tag_name" out of a GitHub releases/latest response.
// Missing tag, bad JSON or an error payload all collapse to "".
func parseReleaseTag(body []byte) string {
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return ""
	}
	return strings.TrimSpace(rel.TagName)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
