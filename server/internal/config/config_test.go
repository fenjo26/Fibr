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

func TestXrayRequiresSNI(t *testing.T) {
	t.Setenv("FIBR_ADMIN_TOKEN", "x")
	t.Setenv("FIBR_SECRET", "x")
	t.Setenv("FIBR_XRAY_ENABLED", "true")
	t.Setenv("FIBR_XRAY_SHORT_ID", "deadbeef")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when FIBR_XRAY_SNI is missing")
	}
}
