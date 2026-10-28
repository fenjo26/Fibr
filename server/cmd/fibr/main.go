// Command fibr is the Fibr server: WireGuard manager, REST API and
// Telegram bot in a single binary.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fenjo26/Fibr/internal/api"
	"github.com/fenjo26/Fibr/internal/bot"
	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/store"
	"github.com/fenjo26/Fibr/internal/wg"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("configuration error", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		log.Error("cannot create data dir", "dir", cfg.DataDir, "error", err)
		os.Exit(1)
	}

	st, err := store.Open(filepath.Join(cfg.DataDir, "fibr.db"))
	if err != nil {
		log.Error("cannot open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	wgm, err := wg.New(cfg, st, log)
	if err != nil {
		log.Error("wireguard manager init failed", "error", err)
		os.Exit(1)
	}
	if err := wgm.EnsureInterface(); err != nil {
		log.Error("wireguard interface setup failed", "error", err)
		os.Exit(1)
	}
	if err := wgm.SyncPeers(); err != nil {
		log.Error("initial peer sync failed (continuing)", "error", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Periodic stats collection (peer transfer counters + iface throughput).
	go func() {
		wgm.CollectStats() // first sample right away
		t := time.NewTicker(cfg.StatsInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				wgm.CollectStats()
			}
		}
	}()

	apiSrv := api.New(cfg, st, wgm, log)

	if cfg.TelegramBotToken != "" {
		b := bot.New(cfg, st, apiSrv.BuildOverview, log)
		go func() {
			if err := b.Run(ctx); err != nil {
				log.Error("telegram bot stopped", "error", err)
			}
		}()
	} else {
		log.Info("FIBR_TELEGRAM_BOT_TOKEN not set, bot and mini app login disabled")
	}

	httpSrv := &http.Server{
		Addr:              cfg.APIAddr,
		Handler:           apiSrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "error", err)
			os.Exit(1)
		}
	}()
	log.Info("fibr api listening", "addr", cfg.APIAddr, "wg_interface", cfg.WGInterface, "passive_mode", wgm.Passive())

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}
