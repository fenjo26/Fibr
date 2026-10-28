// Package config loads Fibr server settings from environment variables.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration.
type Config struct {
	Domain           string        // public domain served by Caddy (may be empty in dev)
	APIAddr          string        // HTTP listen address of the API
	DataDir          string        // persistent state directory (db, keys)
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
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	c := &Config{
		Domain:           strings.TrimSpace(os.Getenv("FIBR_DOMAIN")),
		APIAddr:          getenv("FIBR_API_ADDR", ":8080"),
		DataDir:          getenv("FIBR_DATA_DIR", "./data"),
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
	if c.AdminToken == "" {
		return nil, fmt.Errorf("FIBR_ADMIN_TOKEN is required")
	}
	if c.Secret == "" {
		return nil, fmt.Errorf("FIBR_SECRET is required")
	}

	c.WGPort = getenvInt("FIBR_WG_PORT", 51820)
	if c.WGPort < 1 || c.WGPort > 65535 {
		return nil, fmt.Errorf("FIBR_WG_PORT: %d out of range", c.WGPort)
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
