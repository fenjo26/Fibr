// Package config loads Fibr server settings from environment variables.
package config

import (
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// WGEngine selects the WireGuard engine serving the server's wg interface.
type WGEngine string

const (
	// EngineKernel is the in-kernel WireGuard module (default): fibr creates
	// the interface itself with iproute2 and drives it via wgctrl/netlink.
	EngineKernel WGEngine = "kernel"
	// EngineAWG is AmneziaWG: the bundled amneziawg-go userspace daemon (a
	// wireguard-go fork) owns the TUN interface, and traffic is obfuscated
	// with Jc/Jmin/Jmax/S1/S2/H1-H4 against DPI. Client configs then carry
	// AmneziaWG fields and require AmneziaVPN client apps.
	EngineAWG WGEngine = "awg"
)

// Config holds all runtime configuration.
type Config struct {
	Domain           string        // public domain served by Caddy (may be empty in dev)
	APIAddr          string        // HTTP listen address of the API
	DataDir          string        // persistent state directory (db, keys)
	WGEngine         WGEngine      // FIBR_WG_ENGINE: kernel (default) or awg (AmneziaWG)
	WGInterface      string        // WireGuard interface name (wg0)
	WGPort           int           // WireGuard UDP port, published by docker
	WGHost           string        // public endpoint host written into client configs
	Subnet           string        // VPN IPv4 subnet, /24 only for now
	ServerIP         string        // the server's own address inside Subnet (x.y.z.1)
	Subnet6          string        // VPN IPv6 ULA /64 (dual-stack)
	ServerIP6        string        // the server's ::1 address inside Subnet6
	DNS              []string      // resolvers written into client configs
	MTU              int           // tunnel MTU
	MaxDevices       int           // devices per user
	AdminToken       string        // bearer token for the admin panel and admin API
	Secret           string        // HMAC secret for session tokens
	TelegramBotToken string        // optional; enables the bot and Mini App auth
	AdminTelegramIDs []int64       // telegram user ids auto-promoted to admin
	StatsInterval    time.Duration // peer stats collection period
	RequireInvite    bool          // FIBR_REQUIRE_INVITE: new Mini App users need a one-time invite code
	JoinTTL          time.Duration // FIBR_JOIN_TTL: lifetime of quick-connect codes (default 15m)

	// Xray (VLESS + Reality) — second protocol for DPI resistance. All fields
	// except XrayPort are meaningful only when XrayEnabled is true.
	XrayEnabled bool   // FIBR_XRAY_ENABLED
	XrayPort    int    // FIBR_XRAY_PORT, TCP (Reality masquerades as TLS)
	XraySNI     string // FIBR_XRAY_SNI, camouflage domain, required when enabled
	XrayDest    string // FIBR_XRAY_DEST, Reality fallback target, defaults to SNI:443
	XrayShortID string // FIBR_XRAY_SHORT_ID, hex short id shared by all clients

	// VLESS over XHTTP behind Caddy (docs/xray.md): the same VLESS clients,
	// reached at https://<domain>:443<XHTTPPath> with the domain's own TLS
	// certificate — indistinguishable from the cabinet/mini app traffic that
	// filtered networks already let through. Enabled when FIBR_DOMAIN is a
	// real HTTPS host; XHTTPHost is that host (empty = transport off).
	XHTTPEnabled bool   // FIBR_XHTTP_ENABLED (default true)
	XHTTPPath    string // FIBR_XHTTP_PATH, secret URL path (must match the Caddyfile; default /fibr-xh)
	XHTTPHost    string // derived from FIBR_DOMAIN; "" when the domain is plain http / localhost / an IP

	// Relay (docs/relay.md): a plain TCP/UDP forwarder (nginx stream) on a
	// VPS inside the censored country — its domestic IP is reachable when the
	// main server's foreign hosting range is blocked. When RelayHost is set,
	// client configs of THIS node can point at the relay instead of the
	// server; TLS / WireGuard encryption stays end-to-end to this box.
	RelayHost    string // FIBR_RELAY_HOST: relay IP or hostname; empty = no relay
	RelayDefault bool   // FIBR_RELAY_DEFAULT: configs go via the relay unless the client asks ?via=direct (default true)

	// AWGVersion selects the AmneziaWG parameter generation: 2 (default) adds
	// S3/S4 padding, H1-H4 ranges and the I1 signature packet; 1 keeps the
	// legacy Jc/S1/S2/H1-H4 set (docs/awg.md).
	AWGVersion int    // FIBR_AWG_VERSION
	AWGI1      string // FIBR_AWG_I1: override of the I1 signature packet (client side)

	// AmneziaWG as a per-device protocol: a second tunnel (awg0) next to the
	// plain WireGuard one, on its own UDP port and subnet, served by the
	// bundled amneziawg-go daemon. On by default. When the whole server
	// already runs FIBR_WG_ENGINE=awg the second tunnel is not needed.
	AWGEnabled   bool   // FIBR_AWG_ENABLED (default true)
	AWGPort      int    // FIBR_AWG_PORT, UDP (default 51821)
	AWGInterface string // FIBR_AWG_IFACE (default awg0)
	AWGSubnet    string // FIBR_AWG_SUBNET (default 10.9.0.0/24)
	AWGSubnet6   string // FIBR_AWG_SUBNET6 (default fd42:f1b3::/64)

	// Live control + sharing limit (internal/xray/api.go, limiter.go).
	XrayAPIAddr  string        // FIBR_XRAY_API: xray's gRPC API, default "xray:10085" (compose service); "off" disables
	XrayIPLimit  int           // FIBR_XRAY_IP_LIMIT: distinct source IPs per VLESS link inside the window; 0 = no limit
	XrayIPWindow time.Duration // FIBR_XRAY_IP_WINDOW: how long a seen IP counts (default 2m)
	XrayBan      time.Duration // FIBR_XRAY_BAN: how long an over-limit link stays cut (default 10m)

	// Protected DNS resolver (internal/dns; docs/dns-games.md §4). When
	// DNSEnabled is false the resolver never starts and client configs keep
	// the plain FIBR_DNS resolvers.
	DNSEnabled        bool     // FIBR_DNS_ENABLED (default true)
	DNSDefaultProfile string   // FIBR_DNS_PROFILE_DEFAULT: off | adblock | family (default adblock)
	DNSUpstreams      []string // FIBR_DNS_UPSTREAM, default https://1.1.1.1/dns-query,1.1.1.1
	DNSListsAdblock   string   // FIBR_DNS_LISTS_ADBLOCK, overrides the BuiltIn StevenBlack list
	DNSListsAdult     string   // FIBR_DNS_LISTS_ADULT, overrides the BuiltIn adult list

	// Game catalog (internal/games; docs/dns-games.md §2): the /api/games
	// endpoints serve the catalog for the app's game-tunnel mode.
	GamesEnabled bool // FIBR_GAMES_ENABLED (default true)
}

// DefaultXHTTPPath is the XHTTP path used when FIBR_XHTTP_PATH is unset; the
// Caddyfile carries the same default ({$FIBR_XHTTP_PATH:/fibr-xh}).
const DefaultXHTTPPath = "/fibr-xh"

// XHTTPInternalPort is the XHTTP inbound's port inside the xray container
// (compose network only; Caddy proxies to xray:<port>).
const XHTTPInternalPort = 10086

// DefaultXraySNI is the Reality camouflage host used when FIBR_XRAY_SNI is
// not set (same default as install.sh).
const DefaultXraySNI = "www.microsoft.com"

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		Domain:           strings.TrimSpace(os.Getenv("FIBR_DOMAIN")),
		APIAddr:          getenv("FIBR_API_ADDR", ":8080"),
		DataDir:          getenv("FIBR_DATA_DIR", "./data"),
		WGEngine:         WGEngine(getenv("FIBR_WG_ENGINE", string(EngineKernel))),
		WGInterface:      getenv("FIBR_WG_IFACE", "wg0"),
		WGHost:           strings.TrimSpace(os.Getenv("FIBR_WG_HOST")),
		Subnet:           getenv("FIBR_SUBNET", "10.8.0.0/24"),
		Subnet6:          getenv("FIBR_SUBNET6", "fd42:f1b2::/64"),
		DNS:              splitCSV(getenv("FIBR_DNS", "1.1.1.1,1.0.0.1")),
		MTU:              getenvInt("FIBR_MTU", 1420),
		MaxDevices:       getenvInt("FIBR_MAX_DEVICES", 10),
		AdminToken:       strings.TrimSpace(os.Getenv("FIBR_ADMIN_TOKEN")),
		Secret:           strings.TrimSpace(os.Getenv("FIBR_SECRET")),
		TelegramBotToken: strings.TrimSpace(os.Getenv("FIBR_TELEGRAM_BOT_TOKEN")),
		StatsInterval:    getenvDuration("FIBR_STATS_INTERVAL", 30*time.Second),
		RequireInvite:    getenvBool("FIBR_REQUIRE_INVITE", false),
		JoinTTL:          getenvDuration("FIBR_JOIN_TTL", 15*time.Minute),
	}

	for _, raw := range splitCSV(os.Getenv("FIBR_ADMIN_TELEGRAM_IDS")) {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("FIBR_ADMIN_TELEGRAM_IDS: %q is not a number", raw)
		}
		c.AdminTelegramIDs = append(c.AdminTelegramIDs, id)
	}

	if c.WGHost == "" {
		c.WGHost = c.Domain
	}
	// Dev mode sets FIBR_DOMAIN=http://localhost — the scheme must never
	// leak into client endpoints (wg Endpoint, vless:// links).
	if i := strings.Index(c.WGHost, "://"); i >= 0 {
		c.WGHost = c.WGHost[i+3:]
	}
	c.WGHost = strings.TrimSuffix(strings.TrimSpace(c.WGHost), "/")
	if c.AdminToken == "" {
		return nil, fmt.Errorf("FIBR_ADMIN_TOKEN is required")
	}
	if c.Secret == "" {
		return nil, fmt.Errorf("FIBR_SECRET is required")
	}
	if c.JoinTTL <= 0 {
		return nil, fmt.Errorf("FIBR_JOIN_TTL must be a positive duration")
	}
	switch c.WGEngine {
	case EngineKernel, EngineAWG:
	default:
		return nil, fmt.Errorf("FIBR_WG_ENGINE: %q is not one of kernel, awg", c.WGEngine)
	}

	c.WGPort = getenvInt("FIBR_WG_PORT", 51820)
	if c.WGPort < 1 || c.WGPort > 65535 {
		return nil, fmt.Errorf("FIBR_WG_PORT: %d out of range", c.WGPort)
	}

	// Xray (VLESS + Reality). Port parses even when disabled so a stray
	// FIBR_XRAY_PORT is validated; everything else only matters when enabled.
	// On by default: every user can pick WireGuard or VLESS. Only an
	// explicit FIBR_XRAY_ENABLED=false turns the second protocol off.
	c.XrayEnabled = getenvBool("FIBR_XRAY_ENABLED", true)
	c.XrayPort = getenvInt("FIBR_XRAY_PORT", 8443)
	c.XraySNI = strings.TrimSpace(os.Getenv("FIBR_XRAY_SNI"))
	c.XrayDest = strings.TrimSpace(os.Getenv("FIBR_XRAY_DEST"))
	c.XrayShortID = strings.TrimSpace(os.Getenv("FIBR_XRAY_SHORT_ID"))
	c.XHTTPEnabled = getenvBool("FIBR_XHTTP_ENABLED", true)
	c.XHTTPPath = getenv("FIBR_XHTTP_PATH", DefaultXHTTPPath)
	if !strings.HasPrefix(c.XHTTPPath, "/") {
		c.XHTTPPath = "/" + c.XHTTPPath
	}
	c.XHTTPPath = strings.TrimRight(c.XHTTPPath, "/")
	if c.XHTTPPath == "" || strings.ContainsAny(c.XHTTPPath, " ?#*{}") {
		return nil, fmt.Errorf("FIBR_XHTTP_PATH: %q is not a valid URL path (use e.g. /$(openssl rand -hex 8))", c.XHTTPPath)
	}
	c.XHTTPHost = httpsHost(c.Domain)
	c.RelayHost = strings.TrimSpace(os.Getenv("FIBR_RELAY_HOST"))
	if i := strings.Index(c.RelayHost, "://"); i >= 0 {
		c.RelayHost = c.RelayHost[i+3:]
	}
	c.RelayHost = strings.TrimSuffix(c.RelayHost, "/")
	if strings.ContainsAny(c.RelayHost, " /:") && net.ParseIP(c.RelayHost) == nil {
		return nil, fmt.Errorf("FIBR_RELAY_HOST: %q must be a bare IP or hostname (no port, no path)", c.RelayHost)
	}
	c.RelayDefault = getenvBool("FIBR_RELAY_DEFAULT", true)
	c.AWGVersion = getenvInt("FIBR_AWG_VERSION", 2)
	if c.AWGVersion != 1 && c.AWGVersion != 2 {
		return nil, fmt.Errorf("FIBR_AWG_VERSION: %d is not 1 or 2", c.AWGVersion)
	}
	c.AWGI1 = strings.TrimSpace(os.Getenv("FIBR_AWG_I1"))
	c.AWGEnabled = getenvBool("FIBR_AWG_ENABLED", true)
	c.AWGPort = getenvInt("FIBR_AWG_PORT", 51821)
	c.AWGInterface = getenv("FIBR_AWG_IFACE", "awg0")
	c.AWGSubnet = getenv("FIBR_AWG_SUBNET", "10.9.0.0/24")
	c.AWGSubnet6 = getenv("FIBR_AWG_SUBNET6", "fd42:f1b3::/64")
	if c.AWGPort < 1 || c.AWGPort > 65535 {
		return nil, fmt.Errorf("FIBR_AWG_PORT: %d out of range", c.AWGPort)
	}
	c.XrayAPIAddr = getenv("FIBR_XRAY_API", "xray:10085")
	c.XrayIPLimit = getenvInt("FIBR_XRAY_IP_LIMIT", 2)
	c.XrayIPWindow = getenvDuration("FIBR_XRAY_IP_WINDOW", 2*time.Minute)
	c.XrayBan = getenvDuration("FIBR_XRAY_BAN", 10*time.Minute)

	// Protected DNS + games (docs/dns-games.md §4). Profiles are validated
	// here so a typo fails fast at boot instead of per query.
	c.DNSEnabled = getenvBool("FIBR_DNS_ENABLED", true)
	c.DNSDefaultProfile = getenv("FIBR_DNS_PROFILE_DEFAULT", "adblock")
	switch c.DNSDefaultProfile {
	case "off", "adblock", "family":
	default:
		return nil, fmt.Errorf("FIBR_DNS_PROFILE_DEFAULT: %q is not one of off, adblock, family", c.DNSDefaultProfile)
	}
	c.DNSUpstreams = splitCSV(getenv("FIBR_DNS_UPSTREAM", "https://1.1.1.1/dns-query,1.1.1.1"))
	if len(c.DNSUpstreams) == 0 {
		c.DNSUpstreams = []string{"https://1.1.1.1/dns-query", "1.1.1.1"}
	}
	c.DNSListsAdblock = strings.TrimSpace(os.Getenv("FIBR_DNS_LISTS_ADBLOCK"))
	c.DNSListsAdult = strings.TrimSpace(os.Getenv("FIBR_DNS_LISTS_ADULT"))
	c.GamesEnabled = getenvBool("FIBR_GAMES_ENABLED", true)
	if c.XrayIPLimit < 0 {
		c.XrayIPLimit = 0
	}
	if c.XrayPort < 1 || c.XrayPort > 65535 {
		return nil, fmt.Errorf("FIBR_XRAY_PORT: %d out of range", c.XrayPort)
	}
	if c.XrayEnabled {
		if c.XraySNI == "" {
			c.XraySNI = DefaultXraySNI
		}
		if c.XrayDest == "" {
			c.XrayDest = c.XraySNI + ":443"
		}
		// Empty = generated once by the xray manager and kept in the
		// database (internal/xray); a set value must be valid hex.
		if _, err := hex.DecodeString(c.XrayShortID); err != nil {
			return nil, fmt.Errorf("FIBR_XRAY_SHORT_ID must be even-length hex (generate with: openssl rand -hex 4) or empty")
		}
	}

	serverIP, err := serverIPForSubnet(c.Subnet)
	if err != nil {
		return nil, err
	}
	c.ServerIP = serverIP

	// IPv6 ULA /64: server takes ::1; devices derive their host part from
	// the last IPv4 octet (see IPv6For).
	ip6, ip6net, err := net.ParseCIDR(c.Subnet6)
	if err != nil {
		return nil, fmt.Errorf("FIBR_SUBNET6: %w", err)
	}
	if ip6.To16() == nil || ip6.To4() != nil {
		return nil, fmt.Errorf("FIBR_SUBNET6: an IPv6 ULA /64 is expected, got %s", c.Subnet6)
	}
	if ones, _ := ip6net.Mask.Size(); ones != 64 {
		return nil, fmt.Errorf("FIBR_SUBNET6: only /64 is supported, got %s", c.Subnet6)
	}
	c.Subnet6 = ip6net.String()
	server6 := make(net.IP, len(ip6))
	copy(server6, ip6.To16())
	server6[15] = 1
	c.ServerIP6 = server6.String()

	return c, nil
}

// SecondAWG reports whether the separate AmneziaWG tunnel runs (the whole
// server is not already in awg engine mode).
func (c *Config) SecondAWG() bool { return c.AWGEnabled && c.WGEngine != EngineAWG }

// AWGConfig is the configuration of the second (AmneziaWG) tunnel: a copy
// of this one with the engine, interface, port and subnets swapped. Host,
// DNS, MTU and everything else stay shared.
func (c *Config) AWGConfig() (*Config, error) {
	cc := *c
	cc.WGEngine = EngineAWG
	cc.WGInterface = c.AWGInterface
	cc.WGPort = c.AWGPort
	cc.Subnet = c.AWGSubnet
	cc.Subnet6 = c.AWGSubnet6
	ip, err := serverIPForSubnet(cc.Subnet)
	if err != nil {
		return nil, fmt.Errorf("FIBR_AWG_SUBNET: %w", err)
	}
	cc.ServerIP = ip
	ip6, ip6net, err := net.ParseCIDR(cc.Subnet6)
	if err != nil || ip6.To4() != nil {
		return nil, fmt.Errorf("FIBR_AWG_SUBNET6: an IPv6 ULA /64 is expected, got %s", cc.Subnet6)
	}
	cc.Subnet6 = ip6net.String()
	s6 := make(net.IP, net.IPv6len)
	copy(s6, ip6.To16())
	s6[15] = 1
	cc.ServerIP6 = s6.String()
	return &cc, nil
}

// IPv6For returns the device's IPv6 address: the ULA prefix with the host
// part copied from the last octet of the device's IPv4 (10.8.0.42 ->
// fd42:f1b2::2a). Nothing is stored — the address is always derived.
func (c *Config) IPv6For(ipv4 string) string {
	ip := net.ParseIP(ipv4).To4()
	if ip == nil {
		return ""
	}
	host := make(net.IP, net.IPv6len)
	host[15] = ip[3]
	_, ip6net, err := net.ParseCIDR(c.Subnet6)
	if err != nil {
		return ""
	}
	out := ip6net.IP.To16()
	for i := range host {
		out[i] |= host[i]
	}
	return out.String()
}

// serverIPForSubnet returns the .1 host address of an IPv4 /24 subnet.
func serverIPForSubnet(subnet string) (string, error) {
	ip, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return "", fmt.Errorf("FIBR_SUBNET: %w", err)
	}
	ip = ip.To4()
	if ip == nil {
		return "", fmt.Errorf("FIBR_SUBNET: only IPv4 subnets are supported, got %s", subnet)
	}
	if ones, _ := ipnet.Mask.Size(); ones != 24 {
		return "", fmt.Errorf("FIBR_SUBNET: only /24 subnets are supported in MVP, got %s", subnet)
	}
	server := make(net.IP, len(ip))
	copy(server, ip)
	server[3] = 1
	return server.String(), nil
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getenvDuration(key string, def time.Duration) time.Duration {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// XHTTPActive reports whether the VLESS-over-XHTTP transport (behind Caddy on
// :443) is served and advertised in client links.
func (c *Config) XHTTPActive() bool {
	return c.XrayEnabled && c.XHTTPEnabled && c.XHTTPHost != ""
}

// httpsHost returns the bare host of FIBR_DOMAIN when Caddy serves it over
// HTTPS with a real certificate (a DNS name, no http:// scheme, not
// localhost) — the precondition for the XHTTP transport. Otherwise "".
func httpsHost(domain string) string {
	d := strings.TrimSpace(domain)
	if d == "" || strings.HasPrefix(strings.ToLower(d), "http://") {
		return ""
	}
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	d = strings.TrimSuffix(d, "/")
	if i := strings.IndexByte(d, '/'); i >= 0 {
		d = d[:i]
	}
	host := d
	if h, port, err := net.SplitHostPort(d); err == nil {
		if port != "443" {
			return "" // a non-standard Caddy port: the transport assumes :443
		}
		host = h
	}
	if host == "" || strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return ""
	}
	return host
}
