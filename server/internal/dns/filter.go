package dns

import (
	"io"
	"net"
	"strings"
	"sync"

	"github.com/miekg/dns"
)

// Named rule sets. "adblock", "adult" and "custom" are filled by the list
// manager; "doh" and "safesearch" are built in (filter.go).
const (
	SetAdblock    = "adblock"
	SetAdult      = "adult"
	SetCustom     = "custom"
	SetDoH        = "doh"
	SetSafesearch = "safesearch"
)

// rewriteRule is the parsed form of `$dnsrewrite=NOERROR;<TYPE>;<value>`.
// Only CNAME and A rewrites are supported — exactly what the SafeSearch set
// needs.
type rewriteRule struct {
	rrType uint16 // dns.TypeCNAME or dns.TypeA
	value  string // CNAME target or IPv4 text
}

// ruleSet is one named collection of rules. Domains are stored lowercase with
// the trailing dot stripped; a query matches via an exact hit or a suffix walk
// (www.example.org matches the entry example.org).
type ruleSet struct {
	block   map[string]struct{}
	allow   map[string]struct{} // @@ exceptions, priority over block
	rewrite map[string]rewriteRule
	total   int // rules successfully parsed (diagnostics)
}

func newRuleSet() *ruleSet {
	return &ruleSet{
		block:   map[string]struct{}{},
		allow:   map[string]struct{}{},
		rewrite: map[string]rewriteRule{},
	}
}

// Filter is the process-wide rule store. Sets are replaced atomically under
// an RWMutex so a list refresh never blocks a query behind a write for long
// and never exposes a half-parsed set.
type Filter struct {
	mu   sync.RWMutex
	sets map[string]*ruleSet
}

// NewFilter returns a filter with the built-in "doh" and "safesearch" sets
// loaded.
func NewFilter() *Filter {
	f := &Filter{sets: map[string]*ruleSet{}}
	doh := newRuleSet()
	for _, d := range dohDomains {
		doh.block[d] = struct{}{}
		doh.total++
	}
	f.sets[SetDoH] = doh
	f.sets[SetSafesearch] = safeSearchSet()
	return f
}

// LoadSet parses readers into a single fresh set and atomically replaces the
// named one. Returns the number of rules parsed. Parse errors never happen —
// unrecognized lines are skipped silently — only reader failures propagate.
func (f *Filter) LoadSet(name string, readers ...io.Reader) (int, error) {
	rs := newRuleSet()
	n := 0
	for _, r := range readers {
		parsed, err := parseRules(r, rs)
		n += parsed
		if err != nil {
			return n, err
		}
	}
	rs.total = n
	f.mu.Lock()
	f.sets[name] = rs
	f.mu.Unlock()
	return n, nil
}

// DropSet removes a named set (used when the last custom list is disabled).
func (f *Filter) DropSet(name string) {
	f.mu.Lock()
	delete(f.sets, name)
	f.mu.Unlock()
}

// SetsLen returns rule counts per set name (diagnostics for the admin UI).
func (f *Filter) SetsLen() map[string]int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make(map[string]int, len(f.sets))
	for name, rs := range f.sets {
		out[name] = rs.total
	}
	return out
}

// Match reports whether qname is blocked by any of the named sets. @@
// exceptions in any consulted set win over blocks in all of them.
func (f *Filter) Match(names []string, qname string) (blocked bool, list string) {
	qname = normalizeDomain(qname)
	f.mu.RLock()
	defer f.mu.RUnlock()

	sets := make([]*ruleSet, 0, len(names))
	for _, n := range names {
		if rs, ok := f.sets[n]; ok {
			sets = append(sets, rs)
		}
	}
	for _, rs := range sets {
		if _, ok := matchDomain(rs.allow, qname); ok {
			return false, ""
		}
	}
	for i, rs := range sets {
		if _, ok := matchDomain(rs.block, qname); ok {
			return true, names[i]
		}
	}
	return false, ""
}

// Rewrite looks qname up in the rewrite tables of the named sets.
func (f *Filter) Rewrite(names []string, qname string) (rule rewriteRule, list string, ok bool) {
	qname = normalizeDomain(qname)
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, n := range names {
		rs, exists := f.sets[n]
		if !exists {
			continue
		}
		for d := qname; ; {
			if r, hit := rs.rewrite[d]; hit {
				return r, n, true
			}
			dot := strings.Index(d, ".")
			if dot < 0 || dot == len(d)-1 {
				break
			}
			d = d[dot+1:]
		}
	}
	return rewriteRule{}, "", false
}

// matchDomain walks qname right-to-left by label ("a.b.c" -> "b.c" -> "c")
// and returns the entry that matched.
func matchDomain(set map[string]struct{}, qname string) (string, bool) {
	for d := qname; ; {
		if _, ok := set[d]; ok {
			return d, true
		}
		dot := strings.Index(d, ".")
		if dot < 0 || dot == len(d)-1 {
			return "", false
		}
		d = d[dot+1:]
	}
}

func normalizeDomain(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// parseRules reads rule lines into dst, returning how many were accepted.
// Foreign syntax (CSS selectors, regex rules, comments, malformed junk) is
// skipped silently, which is what makes AdGuard-format lists drop-in
// compatible.
func parseRules(r io.Reader, dst *ruleSet) (int, error) {
	data, err := io.ReadAll(io.LimitReader(r, 128<<20))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, raw := range strings.Split(string(data), "\n") {
		if parseRuleLine(raw, dst) {
			n++
		}
	}
	return n, nil
}

// parseRuleLine parses one line, mutating dst. It reports whether a rule was
// added.
func parseRuleLine(raw string, dst *ruleSet) bool {
	line := strings.TrimSpace(raw)
	if line == "" {
		return false
	}
	// Comments and headers.
	switch line[0] {
	case '#', '!', '[':
		return false
	case '/':
		return false // regex rule
	}
	// Cosmetic (CSS) rules — the other half of why AdGuard lists parse clean.
	if strings.Contains(line, "##") || strings.Contains(line, "#$#") || strings.Contains(line, "#?#") {
		return false
	}

	isException := false
	if strings.HasPrefix(line, "@@") {
		isException = true
		line = strings.TrimPrefix(line, "@@")
	}

	// Split $options off the pattern, if any.
	pattern, opts, hasOpts := strings.Cut(line, "$")

	domain, ok := patternDomain(pattern)
	if !ok {
		return false
	}

	// $dnsrewrite turns the rule into a rewrite; for exceptions and plain
	// options ($script, $image, …) we keep the domain-level meaning. A
	// dnsrewrite we cannot express (other rcode/type) is skipped: it is not
	// a block.
	if hasOpts && !isException {
		if rr, ok := parseDNSRewrite(opts); ok {
			dst.rewrite[domain] = rr
			return true
		}
		if strings.Contains(opts, "dnsrewrite=") {
			return false
		}
	}

	if isException {
		dst.allow[domain] = struct{}{}
	} else {
		dst.block[domain] = struct{}{}
	}
	return true
}

// patternDomain extracts the blocked domain from a rule pattern: hosts format
// ("0.0.0.0 example.org"), plain domain ("example.org") or AdGuard
// ("||example.org^").
func patternDomain(pattern string) (string, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", false
	}

	var domain string
	if strings.HasPrefix(pattern, "||") {
		d := strings.TrimPrefix(pattern, "||")
		// Drop anything after the separator marker: ||example.org^more
		if i := strings.IndexByte(d, '^'); i >= 0 {
			d = d[:i]
		}
		domain = d
	} else if strings.ContainsAny(pattern, "/*") {
		return "", false // URLs and regex-ish junk
	} else if fields := strings.Fields(pattern); len(fields) == 1 {
		domain = fields[0]
	} else if ip := net.ParseIP(strings.Trim(fields[0], "[]")); ip != nil {
		// hosts format: "<ip> <domain> [comment...]"
		domain = fields[1]
	} else {
		return "", false
	}

	domain, ok := validDomain(domain)
	if !ok {
		return "", false
	}
	// hosts lists always carry the loopback block; blocking these would break
	// name resolution on the client itself.
	if _, isLocal := localNames[domain]; isLocal {
		return "", false
	}
	return domain, true
}

// localNames are loopback/link-local names that hosts-format lists ship in
// their header; they must never be filtered.
var localNames = map[string]struct{}{
	"localhost":             {},
	"localhost.localdomain": {},
	"local":                 {},
	"broadcasthost":         {},
	"ip6-localhost":         {},
	"ip6-loopback":          {},
	"ip6-localnet":          {},
	"ip6-mcastprefix":       {},
	"ip6-allnodes":          {},
	"ip6-allrouters":        {},
	"ip6-allhosts":          {},
}

// validDomain normalizes and sanity-checks a candidate domain.
func validDomain(s string) (string, bool) {
	s = strings.ToLower(strings.Trim(s, "."))
	if s == "" || len(s) > 253 {
		return "", false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) < 1 || len(label) > 63 {
			return "", false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			switch {
			case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c == '-', c == '_':
			default:
				return "", false
			}
		}
	}
	return s, true
}

// parseDNSRewrite parses the options part of a rule for
// $dnsrewrite=NOERROR;<TYPE>;<value>. Only NOERROR with CNAME or A is
// understood; anything else is not a rewrite we support.
func parseDNSRewrite(opts string) (rewriteRule, bool) {
	for _, opt := range strings.Split(opts, ",") {
		val, ok := strings.CutPrefix(strings.TrimSpace(opt), "dnsrewrite=")
		if !ok {
			continue
		}
		parts := strings.Split(val, ";")
		if len(parts) != 3 || parts[0] != "NOERROR" {
			return rewriteRule{}, false
		}
		switch parts[1] {
		case "CNAME":
			if d, ok := validDomain(parts[2]); ok {
				return rewriteRule{rrType: dns.TypeCNAME, value: dns.Fqdn(d)}, true
			}
		case "A":
			if ip := net.ParseIP(parts[2]).To4(); ip != nil {
				return rewriteRule{rrType: dns.TypeA, value: ip.String()}, true
			}
		}
		return rewriteRule{}, false
	}
	return rewriteRule{}, false
}

// dohDomains is the built-in DoH-bypass blocklist: well-known public
// DNS-over-HTTPS endpoints. Blocking them (plus the use-application-dns.net
// canary, handled in server.go) keeps clients from routing their resolution
// around Fibr's filtering via encrypted DNS.
var dohDomains = []string{
	"cloudflare-dns.com",
	"security.cloudflare-dns.com",
	"mozilla.cloudflare-dns.com",
	"dns.google",
	"dns.google.com",
	"dns.quad9.net",
	"dns9.quad9.net",
	"doh.opendns.com",
	"dns.adguard.com",
	"dns.mullvad.net",
	"doh.mullvad.net",
	"dns.nextdns.io",
	"dns.sb",
	"doh.cleanbrowsing.org",
	"doh.xfinity.com",
	"dnsoverdns.xfinity.com",
	"dns.digitale-gesellschaft.ch",
	"doh.li",
	"dns.twnic.tw",
	"doh.powerdns.com",
	"dns.rubyfish.cn",
}

// safeSearchSet builds the built-in SafeSearch rewrite set: search engines
// are pinned to their family-safe endpoints via CNAME, Yandex (which has no
// CNAME-based mode) via an A record.
func safeSearchSet() *ruleSet {
	rs := newRuleSet()
	cname := func(domain, target string) {
		rs.rewrite[domain] = rewriteRule{rrType: dns.TypeCNAME, value: dns.Fqdn(target)}
	}
	cname("google.com", "forcesafesearch.google.com")
	cname("youtube.com", "restrictmoderate.youtube.com")
	cname("youtube-nocookie.com", "restrictmoderate.youtube.com")
	cname("bing.com", "strict.bing.com")
	cname("duckduckgo.com", "safe.duckduckgo.com")
	rs.total = len(rs.rewrite)
	yandex := []string{"yandex.ru", "yandex.com", "yandex.by", "yandex.kz"}
	for _, d := range yandex {
		rs.rewrite[d] = rewriteRule{rrType: dns.TypeA, value: "213.180.193.56"}
	}
	rs.total = len(rs.rewrite)
	return rs
}
