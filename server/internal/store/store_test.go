package store

import (
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUserCRUD(t *testing.T) {
	s := openTestStore(t)

	tid := int64(100500)
	u := &User{TelegramID: &tid, Username: "ivan", Role: "user"}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.ID == 0 || u.Role != "user" {
		t.Fatalf("unexpected user after create: %+v", u)
	}

	got, err := s.GetUserByTelegramID(100500)
	if err != nil {
		t.Fatalf("get by telegram id: %v", err)
	}
	if got.Username != "ivan" {
		t.Fatalf("username mismatch: %s", got.Username)
	}

	if err := s.SetUserRole(got.ID, "admin"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	got, _ = s.GetUser(got.ID)
	if got.Role != "admin" {
		t.Fatalf("role not updated: %s", got.Role)
	}

	if err := s.SetUserDisabled(got.ID, true); err != nil {
		t.Fatalf("set disabled: %v", err)
	}
	got, _ = s.GetUser(got.ID)
	if !got.Disabled {
		t.Fatal("disabled flag not persisted")
	}

	if err := s.DeleteUser(got.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetUserByTelegramID(100500); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDevicesAndIPv4Allocation(t *testing.T) {
	s := openTestStore(t)
	u := &User{Username: "alice"}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	mk := func(name string) *Device {
		ip, err := s.NextIPv4("10.8.0.0/24")
		if err != nil {
			t.Fatalf("next ip: %v", err)
		}
		d := &Device{UserID: u.ID, Name: name, PublicKey: "pub-" + name, PrivateKey: "priv", PresharedKey: "psk", IPv4: ip, Enabled: true}
		if err := s.CreateDevice(d); err != nil {
			t.Fatalf("create device: %v", err)
		}
		return d
	}

	d1 := mk("phone") // 10.8.0.2
	d2 := mk("laptop") // 10.8.0.3
	if d1.IPv4 != "10.8.0.2" || d2.IPv4 != "10.8.0.3" {
		t.Fatalf("allocation order: %s then %s", d1.IPv4, d2.IPv4)
	}

	// Deleting the first device must free its address for reuse.
	if err := s.DeleteDevice(d1.ID); err != nil {
		t.Fatalf("delete device: %v", err)
	}
	d3 := mk("tablet")
	if d3.IPv4 != "10.8.0.2" {
		t.Fatalf("expected freed address reuse, got %s", d3.IPv4)
	}

	// Stats update round-trip.
	if err := s.UpdateDeviceStats(d3.ID, d3.CreatedAt, 100, 200, 100, 200); err != nil {
		t.Fatalf("update stats: %v", err)
	}
	got, err := s.GetDeviceByKey(d3.PublicKey)
	if err != nil {
		t.Fatalf("get by key: %v", err)
	}
	if got.TotalRx != 100 || got.TotalTx != 200 || got.LastHandshake.IsZero() {
		t.Fatalf("stats mismatch: %+v", got)
	}

	// Cascade: deleting the user removes devices.
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if _, err := s.GetDevice(d3.ID); err != ErrNotFound {
		t.Fatalf("expected cascade delete, got %v", err)
	}
}

func TestSettings(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.GetSetting("k"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.SetSetting("k", "v1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetSetting("k", "v2"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	v, err := s.GetSetting("k")
	if err != nil || v != "v2" {
		t.Fatalf("get: %q err=%v", v, err)
	}
}
