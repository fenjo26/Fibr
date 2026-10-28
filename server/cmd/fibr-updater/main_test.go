package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The helpers are tested on fake git/GitHub output — no real git, no network
// (CI and dev machines must not need either).
func TestParseLSRemote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"3f2b1a0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4\trefs/heads/main\n", "3f2b1a0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4"},
		{"3f2b1a0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4\tHEAD\n", "3f2b1a0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4"},
		{"3f2b1a0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4", "3f2b1a0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4"}, // bare hash, no ref
		{"  \n3f2b1a0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4\trefs/heads/main\n", ""},                 // blank first line (git never emits this)
		{"", ""},
		{"\n", ""},
		{"   ", ""},
		{"fatal: could not read from remote repository.\n", "fatal:"},
	}
	for _, c := range cases {
		if got := parseLSRemote(c.in); got != c.want {
			t.Errorf("parseLSRemote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.2.3\nmore\n", "v1.2.3"},
		{"  v1.2.3-dirty  \n", "v1.2.3-dirty"},
		{"abc1234", "abc1234"},
		{"\n", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := firstLine(c.in); got != c.want {
			t.Errorf("firstLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLastLines(t *testing.T) {
	in := "l1\nl2\nl3\nl4\n"
	if got := lastLines(in, 2); got != "l3\nl4" {
		t.Errorf("lastLines(4 lines, 2) = %q", got)
	}
	if got := lastLines(in, 10); got != "l1\nl2\nl3\nl4" {
		t.Errorf("lastLines(4 lines, 10) = %q", got)
	}
	if got := lastLines("", 5); got != "" {
		t.Errorf("lastLines(empty) = %q", got)
	}
}

func TestParseReleaseTag(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"ok", `{"tag_name":"v1.4.0","name":"Fibr 1.4.0"}`, "v1.4.0"},
		{"padded", `{"tag_name":"  v1.4.0\n"}`, "v1.4.0"},
		{"missing tag", `{"message":"Not Found"}`, ""},
		{"garbage", `{"tag_name":`, ""},
		{"not json", `rate limited`, ""},
	}
	for _, c := range cases {
		if got := parseReleaseTag([]byte(c.in)); got != c.want {
			t.Errorf("parseReleaseTag(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("FIBR_TEST_UPDATER_VAR", "x")
	if got := envOr("FIBR_TEST_UPDATER_VAR", "def"); got != "x" {
		t.Errorf("envOr set = %q", got)
	}
	if got := envOr("FIBR_TEST_UPDATER_UNSET", "def"); got != "def" {
		t.Errorf("envOr unset = %q", got)
	}
	t.Setenv("FIBR_TEST_UPDATER_BLANK", "  ")
	if got := envOr("FIBR_TEST_UPDATER_BLANK", "def"); got != "def" {
		t.Errorf("envOr blank = %q", got)
	}
}

// A killed update (OOM, restart) must not 409 forever: a lock older than
// staleLock is reclaimed by the next attempt.
func TestTryLockStaleReclaim(t *testing.T) {
	s := &server{lockfile: filepath.Join(t.TempDir(), "updating")}

	// Fresh lock: first take wins, a second one conflicts, /status sees it.
	lock, err := s.tryLock()
	if err != nil {
		t.Fatalf("tryLock: %v", err)
	}
	defer lock.Close()
	if _, err := s.tryLock(); !os.IsExist(err) {
		t.Fatalf("second tryLock: err=%v, want EEXIST", err)
	}
	if !s.updating() {
		t.Fatal("updating() = false with a live lock")
	}

	// Backdate the lock beyond the staleness window: it is reclaimable.
	old := time.Now().Add(-(staleLock + time.Minute))
	if err := os.Chtimes(s.lockfile, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if s.updating() {
		t.Fatal("updating() = true on a stale lock")
	}
	if _, err := s.tryLock(); err != nil {
		t.Fatalf("stale tryLock: %v", err)
	}
}
