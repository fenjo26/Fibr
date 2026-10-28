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
	DNS              []string      // resolvers written into client configs
	MTU              int           // tunnel MTU
	MaxDevices       int           // devices per user
	AdminToken       string        // bearer token for the admin panel and admin API
	Secret           string        // HMAC secret for session tokens
	TelegramBotToken string        // optional; enables the bot and Mini App auth
	AdminTelegramIDs []int64       // telegram user ids auto-promoted to admin
	StatsInterval    time.Duration // peer stats collection period
	RequireInvite    bool          // FIBR_REQUIRE_INVITE: new Mini App users need a one-time invite code

	// Xray (VLESS + Reality) — second protocol for DPI resistance. All fields
	// except XrayPort are meaningful only when XrayEnabled is true.
	XrayEnabled bool   // FIBR_XRAY_ENABLED
	XrayPort    int    // FIBR_XRAY_PORT, TCP (Reality masquerades as TLS)
	XraySNI     string // FIBR_XRAY_SNI, camouflage domain, required when enabled
	XrayDest    string // FIBR_XRAY_DEST, Reality fallback target, defaults to SNI:443
	XrayShortID string // FIBR_XRAY_SHORT_ID, hex short id shared by all clients
}

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
		DNS:              splitCSV(getenv("FIBR_DNS", "1.1.1.1,1.0.0.1")),
		MTU:              getenvInt("FIBR_MTU", 1420),
		MaxDevices:       getenvInt("FIBR_MAX_DEVICES", 10),
		AdminToken:       strings.TrimSpace(os.Getenv("FIBR_ADMIN_TOKEN")),
		Secret:           strings.TrimSpace(os.Getenv("FIBR_SECRET")),
		TelegramBotToken: strings.TrimSpace(os.Getenv("FIBR_TELEGRAM_BOT_TOKEN")),
		StatsInterval:    getenvDuration("FIBR_STATS_INTERVAL", 30*time.Second),
		RequireInvite:    getenvBool("FIBR_REQUIRE_INVITE", false),
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
	c.XrayEnabled = getenvBool("FIBR_XRAY_ENABLED", false)
	c.XrayPort = getenvInt("FIBR_XRAY_PORT", 8443)
	c.XraySNI = strings.TrimSpace(os.Getenv("FIBR_XRAY_SNI"))
	c.XrayDest = strings.TrimSpace(os.Getenv("FIBR_XRAY_DEST"))
	c.XrayShortID = strings.TrimSpace(os.Getenv("FIBR_XRAY_SHORT_ID"))
	if c.XrayPort < 1 || c.XrayPort > 65535 {
		return nil, fmt.Errorf("FIBR_XRAY_PORT: %d out of range", c.XrayPort)
	}
	if c.XrayEnabled {
		if c.XraySNI == "" {
			return nil, fmt.Errorf("FIBR_XRAY_SNI is required when FIBR_XRAY_ENABLED=true (the camouflage domain, e.g. www.microsoft.com)")
		}
		if c.XrayDest == "" {
			c.XrayDest = c.XraySNI + ":443"
		}
		if _, err := hex.DecodeString(c.XrayShortID); err != nil || c.XrayShortID == "" {
			return nil, fmt.Errorf("FIBR_XRAY_SHORT_ID must be non-empty even-length hex (generate with: openssl rand -hex 4)")
		}
	}

	serverIP, err := serverIPForSubnet(c.Subnet)
	if err != nil {
		return nil, err
	}
	c.ServerIP = serverIP
	return c, nil
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
