// Command fibr is the Fibr server: WireGuard manager, REST API and
// Telegram bot in a single binary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fenjo26/Fibr/internal/api"
	"github.com/fenjo26/Fibr/internal/awg"
	"github.com/fenjo26/Fibr/internal/bot"
	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/dns"
	"github.com/fenjo26/Fibr/internal/games"
	"github.com/fenjo26/Fibr/internal/store"
	"github.com/fenjo26/Fibr/internal/wg"
	"github.com/fenjo26/Fibr/internal/xray"
)

// sameIDSet reports whether two id sets hold exactly the same elements.
func sameIDSet(a, b map[int64]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for id := range a {
		if _, ok := b[id]; !ok {
			return false
		}
	}
	return true
}

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

	// Multi-node (see docs/multinode.md): upsert the built-in "local" node
	// row so this box participates in the node registry, with host/ports
	// refreshed from env on every boot. Xray port is recorded only while the
	// xray protocol is enabled.
	var localXrayPort *int
	if cfg.XrayEnabled {
		p := cfg.XrayPort
		localXrayPort = &p
	}
	if _, err := store.EnsureLocalNode(st, cfg.WGHost, cfg.WGPort, localXrayPort); err != nil {
		log.Error("cannot ensure local node", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	wgm, err := wg.New(cfg, st, log)
	if err != nil {
		log.Error("wireguard manager init failed", "error", err)
		os.Exit(1)
	}

	// AmneziaWG engine: the amneziawg-go userspace daemon owns the TUN
	// interface, so it must be up before the manager configures wg0. fibr is
	// the container's init process and stays the daemon's parent: it gets a
	// SIGTERM (then SIGKILL) through the command context on shutdown. The
	// daemon has no config file — keys, port, peers and the AWG obfuscation
	// parameters are all applied over its UAPI socket by the wg manager.
	var awgDaemon *awg.Daemon
	if cfg.WGEngine == config.EngineAWG {
		if wgm.Passive() {
			log.Warn("FIBR_WG_ENGINE=awg ignored: no WireGuard control plane (passive/dev mode), serving configs only")
		} else {
			awgDaemon, err = awg.StartDaemon(ctx, cfg.WGInterface, log)
			if err != nil {
				log.Error("amneziawg daemon start failed", "error", err)
				os.Exit(1)
			}
			if err := awgDaemon.WaitReady(15 * time.Second); err != nil {
				log.Error("amneziawg daemon not ready", "error", err)
				_ = awgDaemon.Stop()
				os.Exit(1)
			}
		}
	}

	if err := wgm.EnsureInterface(); err != nil {
		log.Error("wireguard interface setup failed", "error", err)
		if awgDaemon != nil {
			_ = awgDaemon.Stop()
		}
		os.Exit(1)
	}
	if err := wgm.SyncPeers(); err != nil {
		log.Error("initial peer sync failed (continuing)", "error", err)
	}

	// AmneziaWG as a per-device protocol: a second tunnel next to the plain
	// WireGuard one (own interface/port/subnet, amneziawg-go daemon). Not
	// fatal — WireGuard keeps working if it cannot start.
	var awgm *wg.Manager
	var awg2Daemon *awg.Daemon
	if cfg.SecondAWG() && !wgm.Passive() {
		startAWG := func() error {
			m, err := wg.NewAWG(cfg, st, log)
			if err != nil {
				return err
			}
			d, err := awg.StartDaemon(ctx, cfg.AWGInterface, log)
			if err != nil {
				return err
			}
			if err := d.WaitReady(15 * time.Second); err != nil {
				_ = d.Stop()
				return err
			}
			if err := m.EnsureInterface(); err != nil {
				_ = d.Stop()
				return err
			}
			if err := m.SyncPeers(); err != nil {
				log.Error("initial amneziawg peer sync failed (continuing)", "error", err)
			}
			awgm, awg2Daemon = m, d
			return nil
		}
		if err := startAWG(); err != nil {
			log.Error("amneziawg tunnel failed to start; the awg protocol stays off (wireguard continues)", "error", err)
		} else {
			log.Info("amneziawg tunnel up", "iface", cfg.AWGInterface, "port", cfg.AWGPort, "subnet", cfg.AWGSubnet)
		}
	} else if !cfg.AWGEnabled {
		log.Info("FIBR_AWG_ENABLED=false, amneziawg protocol disabled")
	}

	// Xray (VLESS + Reality): render /data/xray/config.json for the xray
	// container. Errors are not fatal — WireGuard keeps working without xray;
	// if the manager cannot start we hard-disable xray so the API rejects
	// xray requests instead of panicking on a nil manager.
	xm, err := xray.New(cfg, st, log)
	if err != nil {
		log.Error("xray manager init failed, disabling xray (wireguard continues)", "error", err)
		xm = nil
		cfg.XrayEnabled = false
	} else if cfg.XrayEnabled {
		if err := xm.SyncClients(); err != nil {
			log.Error("initial xray config sync failed (continuing)", "error", err)
		}
	} else {
		log.Info("FIBR_XRAY_ENABLED not set, xray protocol disabled")
	}

	// Protected DNS resolver (internal/dns; docs/dns-games.md §1): listens
	// on :53 inside the container, per-device profiles come from the store.
	// Failure is not fatal — the VPN keeps working; client configs then keep
	// the plain FIBR_DNS resolvers (renderCredential checks dnssrv).
	var dnsSrv *dns.Server
	var awgLeak *dns.AntiLeak
	if cfg.DNSEnabled {
		dcfg := dns.DefaultConfig()
		dcfg.DataDir = filepath.Join(cfg.DataDir, "dns")
		dcfg.Upstreams = cfg.DNSUpstreams
		dcfg.DefaultProfile = dns.Profile(cfg.DNSDefaultProfile)
		dcfg.ListsAdblockURL = cfg.DNSListsAdblock
		dcfg.ListsAdultURL = cfg.DNSListsAdult
		if _, tunnelNet, err := net.ParseCIDR(cfg.Subnet); err == nil {
			dcfg.Subnet = *tunnelNet
		}
		dcfg.ProfileLookup = func(clientIP net.IP) (dns.Profile, dns.Schedule, bool) {
			profile, schedJSON, _, ok := st.DeviceDNSProfile(clientIP.String())
			var sched dns.Schedule
			if schedJSON != "" {
				_ = json.Unmarshal([]byte(schedJSON), &sched)
			}
			return dns.Profile(profile), sched, ok
		}
		dcfg.AllowlistLookup = func(clientIP net.IP) []string {
			_, _, allowJSON, ok := st.DeviceDNSProfile(clientIP.String())
			if !ok || allowJSON == "" {
				return nil
			}
			var domains []string
			if json.Unmarshal([]byte(allowJSON), &domains) != nil {
				return nil
			}
			return domains
		}
		srv, err := dns.NewServer(dcfg, log)
		if err != nil {
			log.Error("dns resolver init failed; dns protection stays off (vpn continues)", "error", err)
		} else if err := srv.Start(ctx); err != nil {
			log.Error("dns resolver start failed; dns protection stays off (vpn continues)", "error", err)
		} else {
			dnsSrv = srv
			defer srv.Stop()
			// Client configs switch their DNS line to the tunnel gateway
			// only now that the resolver actually answers (wg.go DNSLine).
			wgm.SetDNSResolver(true)
			if awgm != nil {
				awgm.SetDNSResolver(true)
			}
			log.Info("dns protection up", "addr", srv.Addr(), "default_profile", string(dcfg.DefaultProfile))
			// The resolver's own anti-leak rules cover the wg subnet only;
			// AmneziaWG clients (second tunnel) need the same DNAT from
			// their subnet or their port-53 traffic escapes the filter.
			if awgm != nil {
				if ac, err := cfg.AWGConfig(); err == nil {
					if _, awgNet, err := net.ParseCIDR(ac.Subnet); err == nil {
						awgLeak = dns.NewAntiLeak(*awgNet, net.ParseIP(ac.ServerIP), log)
						if err := awgLeak.Apply(); err != nil {
							log.Warn("dns anti-leak for the amneziawg subnet not applied", "error", err)
							awgLeak = nil
						}
					}
				}
			}
		}
	} else {
		log.Info("FIBR_DNS_ENABLED=false, dns protection disabled")
	}

	// Game catalog (internal/games; docs/dns-games.md §2): serves
	// /api/games for the app's game-tunnel mode. Boots from the builtin
	// snapshot, refreshes from GitHub every 24h.
	var gamesCat *games.Catalog
	if cfg.GamesEnabled {
		gamesCat = games.NewCatalog(games.Options{DataDir: cfg.DataDir})
		go gamesCat.Run(ctx)
		log.Info("game catalog up", "games", len(gamesCat.List()))
	} else {
		log.Info("FIBR_GAMES_ENABLED=false, game catalog disabled")
	}

	// Subscription expiry pass (manual monetization, see docs/billing.md).
	// Runs on every stats tick: recompute the set of expired accounts and
	// resync the tunnels only when that set CHANGED — an account that just
	// crossed its deadline gets its tunnel cut within one interval, while a
	// steady state never rebuilds peers for nothing. Both engines are
	// resynced: WireGuard peers AND the xray client list (an expired user's
	// VLESS credentials must stop working exactly like their wg ones).
	var lastExpired map[int64]struct{}
	checkExpiries := func() {
		ids, err := st.ListExpiredUserIDs(time.Now())
		if err != nil {
			log.Warn("expiry check failed", "error", err)
			return
		}
		cur := make(map[int64]struct{}, len(ids))
		for _, id := range ids {
			cur[id] = struct{}{}
		}
		if sameIDSet(cur, lastExpired) {
			return
		}
		grew := len(cur) > len(lastExpired)
		lastExpired = cur
		if err := wgm.SyncPeers(); err != nil {
			log.Error("peer resync after expiry change failed", "error", err)
			return
		}
		if awgm != nil {
			if err := awgm.SyncPeers(); err != nil {
				log.Error("amneziawg peer resync after expiry change failed", "error", err)
			}
		}
		if xm != nil {
			if err := xm.SyncClients(); err != nil {
				log.Error("xray resync after expiry change failed", "error", err)
			}
		}
		if _, err := st.BumpPeerRevision(); err != nil {
			log.Error("peer revision bump after expiry change failed", "error", err)
		}
		if grew {
			log.Info(fmt.Sprintf("%d users expired, peers resynced", len(cur)))
		} else {
			log.Info("expired set shrank or changed, peers resynced", "expired", len(cur))
		}
	}

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
				if awgm != nil {
					awgm.CollectStats()
				}
				checkExpiries()
			}
		}
	}()

	apiSrv := api.New(cfg, st, wgm, xm, log)
	if awgm != nil {
		apiSrv.SetAWG(awgm)
	}
	apiSrv.SetDNS(dnsSrv)
	apiSrv.SetGames(gamesCat)

	// "One VLESS link = one device": cut links used from too many IPs at
	// once (internal/xray/limiter.go; FIBR_XRAY_IP_LIMIT=0 turns it off).
	if xm != nil {
		go xm.RunLimiter(ctx)
	}

	if cfg.TelegramBotToken != "" {
		b := bot.New(cfg, st, apiSrv.BuildOverview, log)
		// Config files through Telegram: /new in the bot and the Mini App
		// "send file to Telegram" button (internal/api/deliver.go).
		b.SetDeviceIssuer(apiSrv)
		apiSrv.SetFileSender(b.SendFile)
		if xm != nil {
			// Sharing limiter cut a VLESS link: tell its owner and the admins.
			xm.OnLimit(func(uuid string, ips []string) {
				d, err := st.GetDeviceByKey(uuid)
				if err != nil {
					return
				}
				var chat int64
				if u, err := st.GetUser(d.UserID); err == nil && u.TelegramID != nil {
					chat = *u.TelegramID
				}
				b.NotifyLinkShared(chat, d.Name, len(ips), cfg.XrayBan)
			})
		}
		if name, err := b.FetchMe(); err == nil {
			apiSrv.SetBotUsername(name)
		} else {
			log.Warn("cannot fetch bot username; web cabinet widget will be hidden", "error", err)
		}
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
	if awgLeak != nil {
		awgLeak.Remove()
	}
	if awgDaemon != nil {
		_ = awgDaemon.Stop()
	}
	if awg2Daemon != nil {
		_ = awg2Daemon.Stop()
	}
}
