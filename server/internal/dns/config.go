package dns

import (
	"net"
	"time"
)

// Profile is a device's filtering mode (docs/dns-games.md §1).
type Profile string

const (
	// ProfileOff disables filtering entirely: cache + upstream only.
	ProfileOff Profile = "off"
	// ProfileAdblock blocks ads/trackers and DoH-bypass domains.
	ProfileAdblock Profile = "adblock"
	// ProfileFamily is adblock plus the adult list, SafeSearch and the
	// parental schedule.
	ProfileFamily Profile = "family"
)

// Schedule is the parental-control time window applied on top of ProfileFamily.
// Days use ISO numbering: 1=Monday .. 7=Sunday. An empty Days slice means
// "every day". The window may cross midnight (22:00–06:00); start == end means
// "around the clock". The zero value disables the schedule.
type Schedule struct {
	Days  []int  `json:"days"`
	Start string `json:"start"` // "HH:MM"
	End   string `json:"end"`   // "HH:MM"
	TZ    string `json:"tz"`    // IANA name, e.g. "Europe/Berlin"
}

// Config configures the resolver. Only Subnet and ProfileLookup need wiring
// from the rest of the server; everything else has a DefaultConfig value.
type Config struct {
	// Enabled reports whether the resolver starts at all.
	Enabled bool
	// ListenAddr is the plain-DNS UDP/TCP listen address (":53").
	ListenAddr string
	// Upstreams are dnsproxy upstream specs, e.g. "https://1.1.1.1/dns-query"
	// or "1.1.1.1"; the first entry is used when several are given.
	Upstreams []string
	// DefaultProfile is applied to clients ProfileLookup does not know.
	DefaultProfile Profile
	// ListsAdblockURL and ListsAdultURL override the BuiltIn list URLs
	// (FIBR_DNS_LISTS_ADBLOCK / FIBR_DNS_LISTS_ADULT).
	ListsAdblockURL string
	ListsAdultURL   string
	// DataDir keeps lists.db, queries.db and the downloaded list cache.
	DataDir string
	// QueryLogRetention prunes dns_query_log rows older than this.
	QueryLogRetention time.Duration
	// Subnet is the WireGuard tunnel network (10.8.0.0/24): the source
	// selector for the anti-leak iptables rules.
	Subnet net.IPNet
	// ProfileLookup resolves a client's tunnel IP to its profile and
	// schedule. Nil or !ok falls back to DefaultProfile.
	ProfileLookup func(clientIP net.IP) (Profile, Schedule, bool)
}

// DefaultConfig returns the production defaults (env plumbing lives in
// internal/config, docs/dns-games.md §4):
//
//	:53 listener, Cloudflare DoH + 1.1.1.1 upstreams, adblock profile,
//	StevenBlack lists, 7-day query log.
func DefaultConfig() Config {
	_, subnet, err := net.ParseCIDR("10.8.0.0/24")
	if err != nil {
		panic(err) // constant is valid by construction
	}
	return Config{
		Enabled:           true,
		ListenAddr:        ":53",
		Upstreams:         []string{"https://1.1.1.1/dns-query", "1.1.1.1"},
		DefaultProfile:    ProfileAdblock,
		ListsAdblockURL:   "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
		ListsAdultURL:     "https://raw.githubusercontent.com/StevenBlack/hosts/master/alternates/porn/hosts",
		QueryLogRetention: 7 * 24 * time.Hour,
		Subnet:            *subnet,
	}
}
