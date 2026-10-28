// Command fibr-node is the Fibr remote node agent (multi-node mode; see
// docs/multinode.md). It runs on a satellite box, owns a WireGuard interface
// (and optionally an xray container), pulls the desired peer list from the
// control plane and reports live peer stats back.
//
// Configuration is environment-only (the node has no database):
//
//	FIBR_API_URL        control plane base URL, e.g. https://vpn.example.com
//	FIBR_NODE_TOKEN     the node token shown once by the admin panel
//	FIBR_WG_IFACE       interface name (default fibr0)
//	FIBR_LISTEN_PORT    WireGuard UDP port (default 51820)
//	FIBR_DATA_DIR       state directory for keys/revision (default /data)
//	FIBR_SYNC_INTERVAL  pull interval (default 20s)
//	FIBR_PASSIVE        "1" = skip all interface operations (tests/CI)
//	FIBR_XRAY_ENABLED   "1" = also render an xray server config from the
//	                    UUIDs the control plane sends
//	FIBR_MTU            tunnel MTU (default 1420, mirrored from the hub)
//	FIBR_AGENT_ADDR     local liveness listener (default :8081); used only
//	                    by the container healthcheck, never published
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := LoadConfig()
	if err != nil {
		log.Error("configuration error", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		log.Error("cannot create data dir", "dir", cfg.DataDir, "error", err)
		os.Exit(1)
	}

	ag, err := NewAgent(cfg, log)
	if err != nil {
		log.Error("agent init failed", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Liveness endpoint for the compose healthcheck. Deliberately dumb —
	// "the process is up" — a node that cannot reach the control plane is
	// the hub's problem to show (last_seen), not a container restart.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: cfg.AgentAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("health listener failed", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	// Registration must succeed before anything else: an unregistered node
	// has no credentials in the control plane's registry and its peers would
	// be rejected. 409 (key mismatch) is fatal — the operator must re-create
	// the node; everything else retries with backoff.
	if err := ag.Register(ctx); err != nil {
		log.Error("registration failed", "error", err)
		os.Exit(1)
	}
	log.Info("node registered", "api", cfg.APIURL, "iface", cfg.Iface, "listen_port", cfg.ListenPort, "passive", cfg.Passive)

	if err := ag.wg.EnsureInterface("", ""); err != nil {
		// Non-fatal on passive; on a real node this means no tunnel — the
		// loop keeps trying to sync so the operator sees it in last_seen.
		log.Error("interface setup failed (continuing in degraded mode)", "error", err)
	}

	// First tick right away so the node serves peers immediately, then the
	// steady loop.
	if err := ag.Tick(ctx); err != nil {
		log.Warn("first sync failed", "error", err)
	}
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("shutting down")
			return
		case <-t.C:
			if err := ag.Tick(ctx); err != nil {
				log.Warn("sync failed", "error", err)
			}
		}
	}
}

// Config holds the agent's environment configuration.
type Config struct {
	APIURL      string
	Token       string
	Iface       string
	ListenPort  int
	XrayPort    int // local xray listen port (must match the node row's xray_port)
	DataDir     string
	Interval    time.Duration
	Passive     bool
	XrayEnabled bool
	MTU         int
	AgentAddr   string // local health listener
}

// LoadConfig reads the agent environment. Deliberately separate from the
// control plane's internal/config: the node binary must never grow
// control-plane knobs, and vice versa.
func LoadConfig() (Config, error) {
	cfg := Config{
		APIURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("FIBR_API_URL")), "/"),
		Token:       strings.TrimSpace(os.Getenv("FIBR_NODE_TOKEN")),
		Iface:       getenv("FIBR_WG_IFACE", "fibr0"),
		ListenPort:  getenvInt("FIBR_LISTEN_PORT", 51820),
		XrayPort:    getenvInt("FIBR_XRAY_PORT", 8443),
		DataDir:     getenv("FIBR_DATA_DIR", "/data"),
		Interval:    getenvDuration("FIBR_SYNC_INTERVAL", 20*time.Second),
		Passive:     os.Getenv("FIBR_PASSIVE") == "1",
		XrayEnabled: os.Getenv("FIBR_XRAY_ENABLED") == "1",
		MTU:         getenvInt("FIBR_MTU", 1420),
		AgentAddr:   getenv("FIBR_AGENT_ADDR", ":8081"),
	}
	if cfg.APIURL == "" {
		return cfg, fmt.Errorf("FIBR_API_URL is required")
	}
	if cfg.Token == "" {
		return cfg, fmt.Errorf("FIBR_NODE_TOKEN is required")
	}
	if cfg.ListenPort < 1 || cfg.ListenPort > 65535 {
		return cfg, fmt.Errorf("FIBR_LISTEN_PORT: %d out of range", cfg.ListenPort)
	}
	if cfg.XrayPort < 1 || cfg.XrayPort > 65535 {
		return cfg, fmt.Errorf("FIBR_XRAY_PORT: %d out of range", cfg.XrayPort)
	}
	if cfg.Interval < time.Second {
		return cfg, fmt.Errorf("FIBR_SYNC_INTERVAL must be at least 1s")
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}

func getenvDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

// httpClient is shared so tests can swap the transport if needed.
func httpClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}
