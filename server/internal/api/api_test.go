package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fenjo26/Fibr/internal/config"
	"github.com/fenjo26/Fibr/internal/store"
	"github.com/fenjo26/Fibr/internal/wg"
	"github.com/fenjo26/Fibr/internal/xray"
)

// newTestAPI builds a fully wired API over temp storage (wg in passive mode
// on non-Linux, which is fine — we only exercise config rendering here).
func newTestAPI(t *testing.T, xrayEnabled bool) (*API, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{
		Domain:      "vpn.example.com",
		WGHost:      "vpn.example.com",
		WGPort:      51820,
		WGInterface: "wg0",
		Subnet:      "10.8.0.0/24",
		ServerIP:    "10.8.0.1",
		DNS:         []string{"1.1.1.1"},
		MTU:         1420,
		MaxDevices:  10,
		AdminToken:  "test-admin-token",
		Secret:      "test-secret",

		XrayEnabled: xrayEnabled,
		XrayPort:    8443,
		XraySNI:     "www.microsoft.com",
		XrayDest:    "www.microsoft.com:443",
		XrayShortID: "0123abcd",
		DataDir:     dir,
	}
	wgm, err := wg.New(cfg, st, slog.Default())
	if err != nil {
		t.Fatalf("wg manager: %v", err)
	}
	xm, err := xray.New(cfg, st, slog.Default())
	if err != nil {
		t.Fatalf("xray manager: %v", err)
	}
	return New(cfg, st, wgm, xm, slog.Default()), st
}

func TestNormalizeProtocol(t *testing.T) {
	cases := []struct {
		in          string
		xrayEnabled bool
		want        string
		wantErr     bool
	}{
		{in: "", xrayEnabled: false, want: "wg"},
		{in: "wg", xrayEnabled: false, want: "wg"},
		{in: "xray", xrayEnabled: false, wantErr: true},
		{in: "xray", xrayEnabled: true, want: "xray"},
		{in: "openvpn", xrayEnabled: true, wantErr: true},
		{in: "Xray", xrayEnabled: true, wantErr: true}, // case-sensitive
	}
	for _, c := range cases {
		got, err := normalizeProtocol(c.in, c.xrayEnabled)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizeProtocol(%q, %v): expected error, got %q", c.in, c.xrayEnabled, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("normalizeProtocol(%q, %v) = %q, %v; want %q", c.in, c.xrayEnabled, got, err, c.want)
		}
	}
}

// TestDeviceLifecycleProtocols walks the user endpoints end to end: create a
// wg and an xray device over HTTP, then fetch each config and QR.
func TestDeviceLifecycleProtocols(t *testing.T) {
	a, st := newTestAPI(t, true)

	u := &store.User{Username: "alice"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("user: %v", err)
	}
	tok := a.newSessionToken(u.ID)
	auth := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }

	create := func(body string) (int, deviceOut) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/me/devices", strings.NewReader(body))
		auth(req)
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		var out deviceOut
		if rec.Code == http.StatusCreated {
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("bad device json: %v: %s", err, rec.Body.String())
			}
		}
		return rec.Code, out
	}

	// wg device (explicit protocol).
	code, wgDev := create(`{"name":"laptop","protocol":"wg"}`)
	if code != http.StatusCreated {
		t.Fatalf("create wg device: status %d", code)
	}
	if wgDev.Protocol != "wg" || wgDev.PublicKey == "" {
		t.Fatalf("wg device out mismatch: %+v", wgDev)
	}

	// xray device: public key must be a uuid, private/psk empty.
	code, xDev := create(`{"name":"pixel","protocol":"xray"}`)
	if code != http.StatusCreated {
		t.Fatalf("create xray device: status %d", code)
	}
	if xDev.Protocol != "xray" || !strings.Contains(xDev.PublicKey, "-") || len(xDev.PublicKey) != 36 {
		t.Fatalf("xray device uuid mismatch: %+v", xDev)
	}
	raw, err := st.GetDevice(xDev.ID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if raw.PrivateKey != "" || raw.PresharedKey != "" {
		t.Fatalf("xray device must keep private_key/preshared_key empty: %+v", raw)
	}

	// Config endpoints.
	fetch := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		auth(req)
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		return rec
	}

	wgCfg := fetch("/api/me/devices/1/config")
	if wgCfg.Code != http.StatusOK || !strings.Contains(wgCfg.Body.String(), "[Interface]") {
		t.Fatalf("wg config endpoint: status %d body %s", wgCfg.Code, wgCfg.Body.String())
	}
	if ct := wgCfg.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("wg config content type: %s", ct)
	}

	xLink := fetch("/api/me/devices/2/config")
	if xLink.Code != http.StatusOK {
		t.Fatalf("xray config endpoint: status %d body %s", xLink.Code, xLink.Body.String())
	}
	link := xLink.Body.String()
	for _, want := range []string{
		"vless://" + xDev.PublicKey + "@vpn.example.com:8443",
		"security=reality", "sni=www.microsoft.com", "sid=0123abcd",
		"flow=xtls-rprx-vision", "encryption=none", "pbk=",
	} {
		if !strings.Contains(link, want) {
			t.Errorf("vless link missing %q: %s", want, link)
		}
	}
	if ct := xLink.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("xray config content type: %s", ct)
	}

	xQR := fetch("/api/me/devices/2/qr.png")
	if xQR.Code != http.StatusOK || !strings.HasPrefix(xQR.Header().Get("Content-Type"), "image/png") {
		t.Fatalf("xray qr endpoint: status %d type %s", xQR.Code, xQR.Header().Get("Content-Type"))
	}

	// The rendered server config on disk must contain the uuid (call the
	// sync directly — the API wrapper is fire-and-forget).
	if err := a.xray.SyncClients(); err != nil {
		t.Fatalf("xray sync: %v", err)
	}
	data, err := os.ReadFile(a.xray.ConfigPath())
	if err != nil {
		t.Fatalf("xray server config not written: %v", err)
	}
	if !strings.Contains(string(data), xDev.PublicKey) {
		t.Fatalf("uuid missing from /data/xray/config.json:\n%s", data)
	}
}

// TestXrayDisabledRejectsDevices: with FIBR_XRAY_ENABLED=false the API must
// refuse to provision xray devices and refuse to serve configs for any that
// exist (e.g. created before the flag was turned off).
func TestXrayDisabledRejectsDevices(t *testing.T) {
	a, st := newTestAPI(t, false)

	u := &store.User{Username: "bob"}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("user: %v", err)
	}
	tok := a.newSessionToken(u.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/me/devices", strings.NewReader(`{"name":"p","protocol":"xray"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for xray on disabled server, got %d: %s", rec.Code, rec.Body.String())
	}

	// Pre-existing xray device (e.g. flag flipped off later): config refused.
	d := &store.Device{UserID: u.ID, Name: "old", Protocol: store.ProtocolXray, PublicKey: xray.NewUUID(), IPv4: "10.8.0.2", Enabled: true}
	if err := st.CreateDevice(d); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/me/devices/1/config", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for xray config on disabled server, got %d", rec.Code)
	}
}
