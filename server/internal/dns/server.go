package dns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/AdguardTeam/dnsproxy/proxy"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/miekg/dns"
)

// canaryDomain is Firefox's application-side DoH canary (RFC
// draft-ietf-add-ddr): answering NXDOMAIN tells the client encrypted DNS is
// unavailable here, so it must keep using plain DNS — which Fibr filters.
const canaryDomain = "use-application-dns.net"

// alwaysAllowed is the schedule allowlist: the family profile's REFUSED
// window never applies to it.
const alwaysAllowed = "fibr.local"

// cacheSizeBytes is the dnsproxy response cache budget (~4 MB).
const cacheSizeBytes = 4 << 20

// blockTTL keeps blocked answers short-lived so rule updates propagate fast.
const blockTTL = 10

// Server is the protected DNS resolver. It wraps a dnsproxy instance with
// Fibr's filtering middleware installed ahead of the cache (the same
// architecture AdGuard Home uses): every request passes filterRequest before
// cache and upstreams are consulted.
type Server struct {
	cfg    Config
	log    *slog.Logger
	filter *Filter
	lists  *ListManager
	qlog   *QueryLog
	leak   *AntiLeak
	px     *proxy.Proxy

	// now is swappable for deterministic schedule tests.
	now func() time.Time
}

// NewServer builds the resolver: opens the list and query-log databases in
// DataDir, loads the built-in filter sets and whatever list cache exists
// (offline-friendly), and parses the upstream pool. Call Start to bind.
func NewServer(cfg Config, log *slog.Logger) (*Server, error) {
	if log == nil {
		log = slog.Default()
	}
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("dns: DataDir is required")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("dns: create data dir: %w", err)
	}
	if len(cfg.Upstreams) == 0 {
		cfg.Upstreams = []string{"https://1.1.1.1/dns-query", "1.1.1.1"}
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":53"
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = ProfileAdblock
	}

	filter := NewFilter()
	lists, err := NewListManager(cfg.DataDir, filter, cfg.ListsAdblockURL, cfg.ListsAdultURL, log)
	if err != nil {
		return nil, fmt.Errorf("dns: list manager: %w", err)
	}
	qlog, err := OpenQueryLog(cfg.DataDir, cfg.QueryLogRetention, log)
	if err != nil {
		lists.Close()
		return nil, fmt.Errorf("dns: query log: %w", err)
	}

	host, portStr, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil {
		lists.Close()
		qlog.Close()
		return nil, fmt.Errorf("dns: listen addr: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		lists.Close()
		qlog.Close()
		return nil, fmt.Errorf("dns: listen port %q is invalid", portStr)
	}

	var listenIP net.IP
	if host != "" {
		if listenIP = net.ParseIP(host); listenIP == nil {
			lists.Close()
			qlog.Close()
			return nil, fmt.Errorf("dns: listen host %q is not an IP", host)
		}
	}

	s := &Server{
		cfg:    cfg,
		log:    log,
		filter: filter,
		lists:  lists,
		qlog:   qlog,
		leak: NewAntiLeak(
			cfg.Subnet,
			resolverIPFor(host, cfg.Subnet),
			log,
		),
		now: time.Now,
	}

	upstreamConf, err := proxy.ParseUpstreamsConfig(cfg.Upstreams, &upstream.Options{Logger: log})
	if err != nil {
		lists.Close()
		qlog.Close()
		return nil, fmt.Errorf("dns: upstreams: %w", err)
	}

	pcfg := &proxy.Config{
		Logger:         log.With("component", "dnsproxy"),
		UDPListenAddr:  []*net.UDPAddr{{IP: listenIP, Port: port}},
		TCPListenAddr:  []*net.TCPAddr{{IP: listenIP, Port: port}},
		UpstreamConfig: upstreamConf,
		CacheEnabled:   true,
		CacheSizeBytes: cacheSizeBytes,
		// Filtering ahead of the cache: our middleware wraps the default
		// handler (cache -> upstream), so blocked answers never pollute the
		// cache and cached answers are only served for allowed queries.
		RequestHandler: s.middleware().Wrap(proxy.DefaultHandler{}),
	}
	px, err := proxy.New(pcfg)
	if err != nil {
		lists.Close()
		qlog.Close()
		return nil, fmt.Errorf("dns: init proxy: %w", err)
	}
	s.px = px
	return s, nil
}

// middleware returns the filtering middleware (docs/dns-games.md §1 order:
// canary -> schedule -> SafeSearch -> block -> upstream).
func (s *Server) middleware() proxy.Middleware {
	return proxy.MiddlewareFunc(func(next proxy.Handler) proxy.Handler {
		return proxy.HandlerFunc(func(ctx context.Context, p *proxy.Proxy, dctx *proxy.DNSContext) error {
			return s.filterRequest(ctx, next, p, dctx)
		})
	})
}

// filterRequest applies the client's profile to one query.
func (s *Server) filterRequest(ctx context.Context, next proxy.Handler, p *proxy.Proxy, dctx *proxy.DNSContext) error {
	if len(dctx.Req.Question) == 0 {
		return next.ServeDNS(ctx, p, dctx)
	}
	q := dctx.Req.Question[0]
	qname := normalizeDomain(q.Name)
	clientIP := clientIPOf(dctx)
	profile, sched := s.profileFor(clientIP)

	entry := LogEntry{
		TS:       s.now(),
		DeviceIP: clientIP.String(),
		QName:    qname,
		QType:    q.Qtype,
		Profile:  string(profile),
	}

	// 1. DoH canary — unconditional, every profile.
	if qname == canaryDomain {
		dctx.Res = rcodeResp(dctx.Req, dns.RcodeNameError)
		entry.Action, entry.List = ActionBlock, "canary"
		s.qlog.Log(entry)
		return nil
	}

	// 2. Parental schedule (family only; the allowlist domain always passes).
	if profile == ProfileFamily && qname != alwaysAllowed && !sched.Allowed(s.now()) {
		dctx.Res = rcodeResp(dctx.Req, dns.RcodeRefused)
		entry.Action = ActionSchedule
		s.qlog.Log(entry)
		return nil
	}

	// 3. SafeSearch rewrites (family; A/AAAA only).
	if profile == ProfileFamily && (q.Qtype == dns.TypeA || q.Qtype == dns.TypeAAAA) {
		if rr, list, ok := s.filter.Rewrite([]string{SetSafesearch}, qname); ok {
			if resp, ok := rewriteResp(dctx.Req, rr); ok {
				dctx.Res = resp
				entry.Action, entry.List = ActionSafesearch, list
				s.qlog.Log(entry)
				return nil
			}
		}
	}

	// 3.5 Personal never-block list (the owner's own exceptions): overrides
	// every block set — including lists the admin enabled — but never the
	// canary, the schedule or SafeSearch.
	if s.allowlisted(clientIP, qname) {
		err := next.ServeDNS(ctx, p, dctx)
		entry.Action, entry.List = ActionAllow, "personal-allow"
		s.qlog.Log(entry)
		return err
	}

	// 4. Profile block sets (off matches nothing, not even DoH).
	if blocked, list := s.filter.Match(profileSets(profile), qname); blocked {
		dctx.Res = blockResp(dctx.Req)
		entry.Action, entry.List = ActionBlock, list
		s.qlog.Log(entry)
		return nil
	}

	// 5. Cache + upstream.
	err := next.ServeDNS(ctx, p, dctx)
	entry.Action = ActionAllow
	s.qlog.Log(entry)
	return err
}

// allowlisted reports whether the client's personal exceptions cover the
// query: an entry "example.com" also covers every subdomain.
func (s *Server) allowlisted(clientIP net.IP, qname string) bool {
	if s.cfg.AllowlistLookup == nil {
		return false
	}
	for _, d := range s.cfg.AllowlistLookup(clientIP) {
		d = normalizeDomain(d)
		if d == "" {
			continue
		}
		if qname == d || strings.HasSuffix(qname, "."+d) {
			return true
		}
	}
	return false
}

// profileSets maps a profile to the block sets it consults. Custom lists act
// in adblock+family; the DoH-bypass list applies to both filtering profiles.
func profileSets(p Profile) []string {
	switch p {
	case ProfileAdblock:
		return []string{SetAdblock, SetCustom, SetDoH}
	case ProfileFamily:
		return []string{SetAdblock, SetCustom, SetDoH, SetAdult}
	default:
		return nil
	}
}

// profileFor resolves the client's profile via the configured lookup,
// falling back to DefaultProfile.
func (s *Server) profileFor(ip net.IP) (Profile, Schedule) {
	if s.cfg.ProfileLookup != nil {
		if p, sched, ok := s.cfg.ProfileLookup(ip); ok && p != "" {
			return p, sched
		}
	}
	return s.cfg.DefaultProfile, Schedule{}
}

// Start binds the listeners, installs the anti-leak rules and launches the
// list-refresh and query-log loops (tied to ctx).
func (s *Server) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		return fmt.Errorf("dns: server is disabled (FIBR_DNS_ENABLED=false)")
	}
	go s.lists.Run(ctx)
	go s.qlog.Run(ctx)
	if err := s.leak.Apply(); err != nil {
		s.log.Warn("dns: anti-leak rules not applied, DNS traffic may bypass the resolver", "error", err)
	}
	if err := s.px.Start(ctx); err != nil {
		return fmt.Errorf("dns: start listeners: %w", err)
	}
	s.log.Info("dns resolver started",
		"listen", s.cfg.ListenAddr,
		"udp_addr", s.px.Addr(proxy.ProtoUDP).String(),
		"default_profile", string(s.cfg.DefaultProfile),
		"sets", s.SetsLen(),
	)
	return nil
}

// Stop shuts everything down and flushes the query log.
func (s *Server) Stop() {
	s.leak.Remove()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.px.Shutdown(ctx); err != nil {
		s.log.Warn("dns: proxy shutdown", "error", err)
	}
	if err := s.qlog.Close(); err != nil {
		s.log.Warn("dns: query log close", "error", err)
	}
	if err := s.lists.Close(); err != nil {
		s.log.Warn("dns: lists close", "error", err)
	}
}

// Addr returns the bound UDP listener address (port 0 turns real here).
func (s *Server) Addr() string {
	if a := s.px.Addr(proxy.ProtoUDP); a != nil {
		return a.String()
	}
	return s.cfg.ListenAddr
}

// Stats24h exports the 24h query statistics (admin UI).
func (s *Server) Stats24h() (Stats, error) { return s.qlog.Stats24h() }

// QueryLogEntries exports recent log rows, optionally per device.
func (s *Server) QueryLogEntries(limit int, deviceIP string) ([]LogEntry, error) {
	return s.qlog.Entries(limit, deviceIP)
}

// SetsLen exports rule counts per set (diagnostics).
func (s *Server) SetsLen() map[string]int { return s.filter.SetsLen() }

// List management delegators for the admin API (docs/dns-games.md §3).
func (s *Server) Lists() []ListEntry             { return s.lists.Lists() }
func (s *Server) AddList(name, url string) error { return s.lists.AddList(name, url) }
func (s *Server) RemoveList(name string) error   { return s.lists.RemoveList(name) }
func (s *Server) SetListEnabled(name string, on bool) error {
	return s.lists.SetListEnabled(name, on)
}
func (s *Server) RefreshAllLists(ctx context.Context) error { return s.lists.RefreshAll(ctx) }
func (s *Server) RefreshOneList(ctx context.Context, name string) error {
	return s.lists.RefreshOne(ctx, name)
}

// clientIPOf extracts the client's IP from the proxy context.
func clientIPOf(dctx *proxy.DNSContext) net.IP {
	ip := net.IP(dctx.Addr.Addr().AsSlice())
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	if ip == nil {
		return net.IPv4zero
	}
	return ip
}

// resolverIPFor picks the address anti-leak redirects to: the resolver's own
// listen IP, or the first host of the tunnel subnet for wildcard binds.
func resolverIPFor(listenHost string, subnet net.IPNet) net.IP {
	if listenHost != "" {
		if ip := net.ParseIP(listenHost); ip != nil {
			if v4 := ip.To4(); v4 != nil && !v4.Equal(net.IPv4zero) {
				return v4
			}
		}
	}
	base := subnet.IP.To4()
	if base == nil {
		return nil
	}
	out := make(net.IP, len(base))
	copy(out, base)
	out[3]++
	return out
}

// rcodeResp builds an error-code answer.
func rcodeResp(req *dns.Msg, rcode int) *dns.Msg {
	m := new(dns.Msg)
	m.SetRcode(req, rcode)
	return m
}

// blockResp answers blocked queries: 0.0.0.0 / :: for address questions (the
// AdGuard Home convention — clients treat it as "nothing here" without
// retrying), NXDOMAIN for everything else.
func blockResp(req *dns.Msg) *dns.Msg {
	q := req.Question[0]
	m := new(dns.Msg)
	switch q.Qtype {
	case dns.TypeA:
		m.SetReply(req)
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: blockTTL},
			A:   net.IPv4zero,
		})
	case dns.TypeAAAA:
		m.SetReply(req)
		m.Answer = append(m.Answer, &dns.AAAA{
			Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: blockTTL},
			AAAA: net.IPv6zero,
		})
	default:
		m.SetRcode(req, dns.RcodeNameError)
	}
	return m
}

// rewriteResp builds a SafeSearch answer from a rewrite rule. A-rewrites only
// apply to A questions; CNAME rewrites apply to both A and AAAA.
func rewriteResp(req *dns.Msg, rr rewriteRule) (*dns.Msg, bool) {
	q := req.Question[0]
	m := new(dns.Msg)
	switch {
	case rr.rrType == dns.TypeCNAME && (q.Qtype == dns.TypeA || q.Qtype == dns.TypeAAAA):
		m.SetReply(req)
		m.Answer = append(m.Answer, &dns.CNAME{
			Hdr:    dns.RR_Header{Name: q.Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: blockTTL},
			Target: rr.value,
		})
	case rr.rrType == dns.TypeA && q.Qtype == dns.TypeA:
		ip := net.ParseIP(rr.value).To4()
		if ip == nil {
			return nil, false
		}
		m.SetReply(req)
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: blockTTL},
			A:   ip,
		})
	default:
		return nil, false
	}
	return m, true
}
