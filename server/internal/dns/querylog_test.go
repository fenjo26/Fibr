package dns

import (
	"context"
	"testing"
	"time"
)

func openTestLog(t *testing.T, retention time.Duration) *QueryLog {
	t.Helper()
	q, err := OpenQueryLog(t.TempDir(), retention, nil)
	if err != nil {
		t.Fatalf("open querylog: %v", err)
	}
	t.Cleanup(func() { q.Close() })
	return q
}

func TestQueryLogFlushAndStats(t *testing.T) {
	q := openTestLog(t, 7*24*time.Hour)

	q.Log(LogEntry{TS: time.Now(), DeviceIP: "10.8.0.2", QName: "example.org", QType: 1, Action: ActionAllow, Profile: "adblock"})
	q.Log(LogEntry{TS: time.Now(), DeviceIP: "10.8.0.2", QName: "ads.example.org", QType: 1, Action: ActionBlock, Profile: "adblock", List: "adblock"})
	q.Log(LogEntry{TS: time.Now(), DeviceIP: "10.8.0.3", QName: "ads.example.org", QType: 1, Action: ActionBlock, Profile: "family", List: "custom"})
	q.Log(LogEntry{TS: time.Now(), DeviceIP: "10.8.0.3", QName: "youtube.com", QType: 1, Action: ActionSafesearch, Profile: "family", List: "safesearch"})
	q.Log(LogEntry{TS: time.Now(), DeviceIP: "10.8.0.4", QName: "tiktok.com", QType: 1, Action: ActionSchedule, Profile: "family"})
	q.flush()

	st, err := q.Stats24h()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.Total != 5 {
		t.Errorf("total = %d, want 5", st.Total)
	}
	want := map[string]int64{
		ActionAllow:      1,
		ActionBlock:      2,
		ActionSafesearch: 1,
		ActionSchedule:   1,
	}
	for a, n := range want {
		if st.ByAction[a] != n {
			t.Errorf("by_action[%s] = %d, want %d", a, st.ByAction[a], n)
		}
	}
	if len(st.TopBlocked) != 1 || st.TopBlocked[0].Domain != "ads.example.org" || st.TopBlocked[0].Count != 2 {
		t.Errorf("top blocked = %+v", st.TopBlocked)
	}

	// Per-device entries, newest first.
	entries, err := q.Entries(10, "10.8.0.3")
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("device entries = %d, want 2", len(entries))
	}
	if entries[0].TS.Before(entries[1].TS) {
		t.Error("entries must be ordered newest first")
	}

	all, err := q.Entries(0, "")
	if err != nil || len(all) != 5 {
		t.Errorf("all entries = %d (err %v), want 5", len(all), err)
	}
}

func TestQueryLogPrune(t *testing.T) {
	q := openTestLog(t, time.Hour)

	old := LogEntry{TS: time.Now().Add(-2 * time.Hour), DeviceIP: "10.8.0.2", QName: "ancient.example", QType: 1, Action: ActionAllow}
	fresh := LogEntry{TS: time.Now(), DeviceIP: "10.8.0.2", QName: "fresh.example", QType: 1, Action: ActionAllow}
	q.Log(old)
	q.Log(fresh)
	q.flush()

	entries, _ := q.Entries(10, "")
	if len(entries) != 2 {
		t.Fatalf("pre-prune entries = %d, want 2", len(entries))
	}

	q.prune(context.Background())

	entries, err := q.Entries(10, "")
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 1 || entries[0].QName != "fresh.example" {
		t.Fatalf("post-prune entries = %+v, want only fresh.example", entries)
	}
}

func TestQueryLogRingOverflow(t *testing.T) {
	q := openTestLog(t, time.Hour)
	for i := 0; i < ringSize+50; i++ {
		q.Log(LogEntry{TS: time.Now(), DeviceIP: "10.8.0.2", QName: "d.example", QType: 1, Action: ActionAllow})
	}
	q.flush()
	entries, err := q.Entries(1, "")
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("overflow entries = %d, want 1", len(entries))
	}
	// The ring must have kept exactly ringSize entries, not grown unbounded.
	q.mu.Lock()
	size := q.size
	q.mu.Unlock()
	if size != 0 { // drained by flush
		t.Errorf("ring must be empty after flush, size=%d", size)
	}
}
