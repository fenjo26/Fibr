package config

import "testing"

func TestWGHostSchemeStripped(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"plain domain":           {"vpn.example.com", "vpn.example.com"},
		"dev scheme":             {"http://localhost", "localhost"},
		"dev scheme with slash":  {"http://localhost/", "localhost"},
		"explicit host override": {"203.0.113.7", "203.0.113.7"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("FIBR_ADMIN_TOKEN", "x")
			t.Setenv("FIBR_SECRET", "x")
			t.Setenv("FIBR_DOMAIN", tc.in)
			c, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if c.WGHost != tc.want {
				t.Fatalf("WGHost = %q, want %q", c.WGHost, tc.want)
			}
		})
	}
}

func TestXrayOnByDefault(t *testing.T) {
	t.Setenv("FIBR_ADMIN_TOKEN", "x")
	t.Setenv("FIBR_SECRET", "x")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.XrayEnabled || c.XraySNI != DefaultXraySNI || c.XrayDest != DefaultXraySNI+":443" || c.XrayShortID != "" {
		t.Fatalf("xray defaults: enabled=%v sni=%q dest=%q sid=%q", c.XrayEnabled, c.XraySNI, c.XrayDest, c.XrayShortID)
	}
	t.Setenv("FIBR_XRAY_SHORT_ID", "nothex")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for a non-hex short id")
	}
	t.Setenv("FIBR_XRAY_SHORT_ID", "")
	t.Setenv("FIBR_XRAY_ENABLED", "false")
	if c, err := Load(); err != nil || c.XrayEnabled {
		t.Fatalf("explicit false must disable: %v %v", c, err)
	}
}

func TestIPv6ForDerivesFromIPv4(t *testing.T) {
	t.Setenv("FIBR_ADMIN_TOKEN", "x")
	t.Setenv("FIBR_SECRET", "x")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ServerIP6 != "fd42:f1b2::1" {
		t.Fatalf("ServerIP6 = %q, want fd42:f1b2::1", c.ServerIP6)
	}
	cases := map[string]string{
		"10.8.0.1":   "fd42:f1b2::1",
		"10.8.0.2":   "fd42:f1b2::2",
		"10.8.0.255": "fd42:f1b2::ff",
		"not-an-ip":  "",
	}
	for v4, want := range cases {
		if got := c.IPv6For(v4); got != want {
			t.Errorf("IPv6For(%q) = %q, want %q", v4, got, want)
		}
	}
}
