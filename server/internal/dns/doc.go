// Package dns implements Fibr's protected DNS resolver: a caching forwarding
// server built on github.com/AdguardTeam/dnsproxy (Apache-2.0) with per-device
// filtering profiles (off / adblock / family), dynamic blocklists, parental
// schedules, SafeSearch enforcement, DoH-bypass prevention and a query log.
//
// The filtering itself is Fibr's own code (docs/dns-games.md §1): the project
// is MIT, so AdGuard's GPL urlfilter library and their filter lists must not
// be used. dnsproxy only provides the DNS plumbing — UDP/TCP listeners, the
// upstream pool (plain DNS and DNS-over-HTTPS) and the response cache.
//
// Wiring it up in cmd/fibr/main.go:
//
//	import "github.com/fenjo26/Fibr/internal/dns"
//
//	cfg := dns.DefaultConfig()
//	cfg.DataDir = filepath.Join(c.DataDir, "dns") // /data/dns in the container
//	cfg.Upstreams = c.DNSUpstreams                // FIBR_DNS_UPSTREAM
//	cfg.Subnet = *tunnelSubnet                    // wg subnet (10.8.0.0/24)
//	// Per-device profiles come from the store once the migration lands
//	// (devices.dns_profile / devices.dns_schedule); until then the callback
//	// may stay nil and every client gets DefaultProfile.
//	cfg.ProfileLookup = func(clientIP net.IP) (dns.Profile, dns.Schedule, bool) {
//		profile, sched, ok := st.DeviceDNSProfile(clientIP.String())
//		return dns.Profile(profile), sched, ok
//	}
//
//	srv, err := dns.NewServer(cfg, logger)
//	if err != nil {
//		log.Fatal(err)
//	}
//	if err := srv.Start(ctx); err != nil {
//		log.Fatal(err)
//	}
//	defer srv.Stop()
//
// Clients get "DNS = <tunnel gateway>" in their WireGuard configs; the
// resolver listens on :53 and the anti-leak rules keep port-53 traffic from
// escaping around it (see antileak.go).
package dns
