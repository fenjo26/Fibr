package dns

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// testUpstream is a plain-DNS resolver answering every A query with a fixed
// address; AAAA gets a fixed v6. Everything else resolves NOERROR/empty.
func testUpstream(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen upstream: %v", err)
	}
	up := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		if len(r.Question) == 1 {
			q := r.Question[0]
			hdr := dns.RR_Header{Name: q.Name, Rrtype: q.Qtype, Class: dns.ClassINET, Ttl: 60}
			switch q.Qtype {
			case dns.TypeA:
				m.Answer = append(m.Answer, &dns.A{Hdr: hdr, A: net.IPv4(192, 0, 2, 7).To4()})
			case dns.TypeAAAA:
				m.Answer = append(m.Answer, &dns.AAAA{Hdr: hdr, AAAA: net.ParseIP("2001:db8::7")})
			}
		}
		_ = w.WriteMsg(m)
	})}
	go func() { _ = up.ActivateAndServe() }()
	t.Cleanup(func() { _ = up.Shutdown() })
	return pc.LocalAddr().String()
}

// startServer brings up a resolver wired to the local upstream and list
// servers; profileFor maps 127.0.0.1 (the test client) to the given profile.
func startServer(t *testing.T, profile Profile, sched Schedule) *Server {
	t.Helper()

	adblockSrv := newListServer("0.0.0.0 ads.example.org\n")
	t.Cleanup(adblockSrv.Close)
	adultSrv := newListServer("0.0.0.0 porn.example.org\n")
	t.Cleanup(adultSrv.Close)
	upstream := testUpstream(t)

	cfg := DefaultConfig()
	cfg.ListenAddr = "127.0.0.1:0"
	cfg.Upstreams = []string{upstream}
	cfg.DefaultProfile = ProfileOff
	cfg.ListsAdblockURL = adblockSrv.URL
	cfg.ListsAdultURL = adultSrv.URL
	cfg.DataDir = t.TempDir()
	cfg.ProfileLookup = func(clientIP net.IP) (Profile, Schedule, bool) {
		return profile, sched, true
	}

	s, err := NewServer(cfg, nil)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Synchronous list load so tests never race the background refresher.
	if err := s.RefreshAllLists(context.Background()); err != nil {
		t.Fatalf("refresh lists: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		s.Stop()
	})
	return s
}

// ask sends one UDP query and returns the response.
func ask(t *testing.T, addr, name string, qtype uint16) *dns.Msg {
	t.Helper()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	c := &dns.Client{Net: "udp", Timeout: 3 * time.Second}
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("exchange %s: %v", name, err)
	}
	return resp
}

func TestServerCanaryNXDOMAIN(t *testing.T) {
	// The canary is answered NXDOMAIN for every profile, including off.
	for _, profile := range []Profile{ProfileOff, ProfileAdblock, ProfileFamily} {
		t.Run(string(profile), func(t *testing.T) {
			s := startServer(t, profile, Schedule{})
			resp := ask(t, s.Addr(), "use-application-dns.net", dns.TypeA)
			if resp.Rcode != dns.RcodeNameError {
				t.Errorf("canary rcode = %d, want NXDOMAIN", resp.Rcode)
			}
		})
	}
}

func TestServerProfiles(t *testing.T) {
	t.Run("off allows everything except the canary", func(t *testing.T) {
		s := startServer(t, ProfileOff, Schedule{})
		for _, d := range []string{"example.org", "ads.example.org", "porn.example.org", "dns.google"} {
			resp := ask(t, s.Addr(), d, dns.TypeA)
			if resp.Rcode != dns.RcodeSuccess {
				t.Errorf("%s: rcode = %d, want NOERROR", d, resp.Rcode)
			}
			if got := resp.Answer; len(got) != 1 {
				t.Errorf("%s: answers = %d, want 1", d, len(got))
			}
		}
	})

	t.Run("adblock blocks ads and doh but not adult", func(t *testing.T) {
		s := startServer(t, ProfileAdblock, Schedule{})
		resp := ask(t, s.Addr(), "ads.example.org", dns.TypeA)
		if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
			t.Fatalf("blocked query shape: rcode=%d answers=%d", resp.Rcode, len(resp.Answer))
		}
		if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4zero) {
			t.Errorf("blocked A = %v, want 0.0.0.0", resp.Answer[0])
		}
		// DoH-bypass list applies.
		resp = ask(t, s.Addr(), "dns.google", dns.TypeA)
		if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4zero) {
			t.Errorf("dns.google must be 0.0.0.0, got %v", resp.Answer[0])
		}
		// The adult list does not apply to adblock.
		resp = ask(t, s.Addr(), "porn.example.org", dns.TypeA)
		if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4(192, 0, 2, 7)) {
			t.Errorf("adult domain must pass adblock, got %v", resp.Answer[0])
		}
		// Subdomain of a blocked domain, via suffix walk.
		resp = ask(t, s.Addr(), "cdn.ads.example.org", dns.TypeA)
		if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4zero) {
			t.Errorf("subdomain must be blocked, got %v", resp.Answer[0])
		}
	})

	t.Run("family blocks adult too", func(t *testing.T) {
		s := startServer(t, ProfileFamily, Schedule{})
		resp := ask(t, s.Addr(), "porn.example.org", dns.TypeA)
		if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4zero) {
			t.Errorf("adult domain must be blocked for family, got %v", resp.Answer[0])
		}
	})

	t.Run("safesearch cname rewrite", func(t *testing.T) {
		s := startServer(t, ProfileFamily, Schedule{})
		resp := ask(t, s.Addr(), "www.google.com", dns.TypeA)
		if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
			t.Fatalf("shape: rcode=%d answers=%d", resp.Rcode, len(resp.Answer))
		}
		cn, ok := resp.Answer[0].(*dns.CNAME)
		if !ok || cn.Target != "forcesafesearch.google.com." {
			t.Errorf("www.google.com -> %v, want CNAME forcesafesearch.google.com.", resp.Answer[0])
		}
		// AAAA of a CNAME-rewritten domain gets the CNAME as well.
		resp = ask(t, s.Addr(), "youtube.com", dns.TypeAAAA)
		if cn, ok := resp.Answer[0].(*dns.CNAME); !ok || cn.Target != "restrictmoderate.youtube.com." {
			t.Errorf("youtube.com AAAA -> %v, want CNAME restrictmoderate.youtube.com.", resp.Answer[0])
		}
		// adblock profile must not get SafeSearch.
		s2 := startServer(t, ProfileAdblock, Schedule{})
		resp = ask(t, s2.Addr(), "www.google.com", dns.TypeA)
		if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4(192, 0, 2, 7)) {
			t.Errorf("adblock profile must resolve google normally, got %v", resp.Answer[0])
		}
	})

	t.Run("safesearch yandex a rewrite", func(t *testing.T) {
		s := startServer(t, ProfileFamily, Schedule{})
		resp := ask(t, s.Addr(), "yandex.ru", dns.TypeA)
		if a, ok := resp.Answer[0].(*dns.A); !ok || !a.A.Equal(net.IPv4(213, 180, 193, 56)) {
			t.Errorf("yandex.ru -> %v, want 213.180.193.56", resp.Answer[0])
		}
	})
}

// denyNow builds a schedule window that provably excludes the current time
// (start = now+1h, end = now+2h, modulo midnight — denied in every case).
func denyNow() Schedule {
	now := time.Now()
	start := now.Add(time.Hour).Format("15:04")
	end := now.Add(2 * time.Hour).Format("15:04")
	return Schedule{Start: start, End: end}
}

func TestServerScheduleRefused(t *testing.T) {
	s := startServer(t, ProfileFamily, denyNow())

	resp := ask(t, s.Addr(), "example.org", dns.TypeA)
	if resp.Rcode != dns.RcodeRefused {
		t.Errorf("outside window rcode = %d, want REFUSED", resp.Rcode)
	}

	// The allowlist domain escapes the schedule.
	resp = ask(t, s.Addr(), "fibr.local", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("fibr.local rcode = %d, want NOERROR", resp.Rcode)
	}

	// Inside the window everything flows again.
	inWindow := startServer(t, ProfileFamily, Schedule{})
	resp = ask(t, inWindow.Addr(), "example.org", dns.TypeA)
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("no schedule rcode = %d, want NOERROR", resp.Rcode)
	}
}

func TestServerQueryLogAndStats(t *testing.T) {
	s := startServer(t, ProfileAdblock, Schedule{})

	ask(t, s.Addr(), "example.org", dns.TypeA)
	ask(t, s.Addr(), "ads.example.org", dns.TypeA)
	ask(t, s.Addr(), "use-application-dns.net", dns.TypeA)

	// The ring flushes every 5s; force it through the public surface by
	// closing (Stop flushes).
	s.qlog.flush()

	entries, err := s.QueryLogEntries(10, "")
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("logged %d queries, want 3: %+v", len(entries), entries)
	}
	byDomain := map[string]LogEntry{}
	for _, e := range entries {
		byDomain[e.QName] = e
	}
	if e := byDomain["example.org"]; e.Action != ActionAllow {
		t.Errorf("example.org action = %q, want allow", e.Action)
	}
	if e := byDomain["ads.example.org"]; e.Action != ActionBlock || e.List != SetAdblock {
		t.Errorf("ads.example.org = %+v, want block/adblock", e)
	}
	if e := byDomain["use-application-dns.net"]; e.Action != ActionBlock || e.List != "canary" {
		t.Errorf("canary = %+v, want block/canary", e)
	}
	if e := byDomain["example.org"]; e.DeviceIP != "127.0.0.1" || e.Profile != string(ProfileAdblock) {
		t.Errorf("entry metadata = %+v", e)
	}

	st, err := s.Stats24h()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.ByAction[ActionBlock] != 2 || st.ByAction[ActionAllow] != 1 {
		t.Errorf("stats by action = %v", st.ByAction)
	}
	if len(st.TopBlocked) != 2 {
		t.Errorf("top blocked = %+v", st.TopBlocked)
	}
}

func TestServerSetsLen(t *testing.T) {
	s := startServer(t, ProfileAdblock, Schedule{})
	lens := s.SetsLen()
	if lens[SetAdblock] == 0 || lens[SetAdult] == 0 {
		t.Errorf("builtin sets not loaded: %v", lens)
	}
	if lens[SetDoH] != len(dohDomains) {
		t.Errorf("doh set = %d, want %d", lens[SetDoH], len(dohDomains))
	}
	if lens[SetSafesearch] == 0 {
		t.Errorf("safesearch set empty: %v", lens)
	}
}
