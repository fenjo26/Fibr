package xray

// Sharing limiter: "one VLESS link = one device".
//
// A vless:// link is a static secret — nothing in the link itself can bind
// it to a phone. What the server CAN see is where connections come from:
// xray's access log names the link (email = uuid) and the source address
// of every connection. The limiter tails that log, remembers each link's
// source IPs for FIBR_XRAY_IP_WINDOW, and when one link is used from more
// distinct IPs than FIBR_XRAY_IP_LIMIT it removes the link from the running
// xray (API RemoveUser) for FIBR_XRAY_BAN, then puts it back.
//
// Limits of the approach, by design: it counts networks, not phones — one
// phone hopping Wi-Fi -> LTE briefly shows two IPs (hence the default of 2
// and the short window), and several people behind one NAT look like one.
// It still stops the real abuse: a link resold to dozens of people is in
// use from dozens of IPs at once.

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fenjo26/Fibr/internal/store"
)

// maxAccessLog: the limiter truncates the access log after reading once it
// grows past this (xray appends with O_APPEND, so writing simply resumes at
// the new end of the file).
const maxAccessLog = 16 << 20

// accessEntry is one parsed access-log line.
type accessEntry struct {
	ip    string
	email string
}

// parseAccessLine extracts the source IP and the link (email) from an xray
// access-log line such as:
//
//	2026/10/28 18:00:00.123456 from 203.0.113.7:51234 accepted tcp:example.com:443 [vless-in -> direct] email: <uuid>
//
// ok is false for lines without both parts (api calls, rejected probes…).
func parseAccessLine(line string) (accessEntry, bool) {
	i := strings.Index(line, " from ")
	j := strings.LastIndex(line, "email: ")
	if i < 0 || j < 0 {
		return accessEntry{}, false
	}
	rest := line[i+len(" from "):]
	src, _, _ := strings.Cut(rest, " ")
	src = strings.TrimPrefix(strings.TrimPrefix(src, "tcp:"), "udp:")
	host, _, err := net.SplitHostPort(src)
	if err != nil {
		host = src
	}
	email := strings.TrimSpace(line[j+len("email: "):])
	if f := strings.Fields(email); len(f) > 0 {
		email = f[0]
	}
	if net.ParseIP(host) == nil || email == "" {
		return accessEntry{}, false
	}
	return accessEntry{ip: host, email: email}, true
}

// ipTracker remembers when each link was last seen from each IP.
type ipTracker struct {
	seen map[string]map[string]time.Time // email -> ip -> last seen
}

func newIPTracker() *ipTracker { return &ipTracker{seen: map[string]map[string]time.Time{}} }

func (t *ipTracker) add(e accessEntry, at time.Time) {
	m := t.seen[e.email]
	if m == nil {
		m = map[string]time.Time{}
		t.seen[e.email] = m
	}
	m[e.ip] = at
}

// over returns the links seen from more than limit IPs within window, with
// those IPs; entries older than the window are dropped on the way.
func (t *ipTracker) over(now time.Time, window time.Duration, limit int) map[string][]string {
	out := map[string][]string{}
	for email, ips := range t.seen {
		for ip, at := range ips {
			if now.Sub(at) > window {
				delete(ips, ip)
			}
		}
		if len(ips) == 0 {
			delete(t.seen, email)
			continue
		}
		if limit > 0 && len(ips) > limit {
			list := make([]string, 0, len(ips))
			for ip := range ips {
				list = append(list, ip)
			}
			sort.Strings(list)
			out[email] = list
		}
	}
	return out
}

// OnLimit registers a callback for every link the limiter cuts (main.go
// notifies the owner and the admins through the bot).
func (m *Manager) OnLimit(f func(uuid string, ips []string)) {
	m.mu.Lock()
	m.onLimit = f
	m.mu.Unlock()
}

// Banned reports whether the link is currently cut by the limiter (and
// until when).
func (m *Manager) Banned(uuid string) (time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	end, ok := m.banned[uuid]
	if !ok || time.Now().After(end) {
		return time.Time{}, false
	}
	return end, true
}

// RunLimiter tails the access log until ctx ends. No-op when xray is
// disabled, the limit is 0 or the live API is off (a cut needs it).
func (m *Manager) RunLimiter(ctx context.Context) {
	if !m.cfg.XrayEnabled || m.cfg.XrayIPLimit <= 0 || m.api == nil {
		return
	}
	path := filepath.Join(m.cfg.DataDir, "xray", "access.log")
	m.log.Info("xray sharing limiter running",
		"ip_limit", m.cfg.XrayIPLimit, "window", m.cfg.XrayIPWindow, "ban", m.cfg.XrayBan)
	tr := newIPTracker()
	var offset int64
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		offset = m.readAccess(path, offset, tr)
		now := time.Now()
		for uuid, ips := range tr.over(now, m.cfg.XrayIPWindow, m.cfg.XrayIPLimit) {
			m.ban(ctx, uuid, ips, now)
			delete(tr.seen, uuid) // start counting afresh after the ban
		}
		m.unbanExpired(ctx, now)
	}
}

// readAccess feeds new log lines into the tracker and returns the new
// offset. A shrunk file (rotated/truncated) is read from the start.
func (m *Manager) readAccess(path string, offset int64, tr *ipTracker) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0 // not there yet: xray has not logged anything
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return offset
	}
	if st.Size() < offset {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset
	}
	now := time.Now()
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			break // a partial last line is re-read next tick
		}
		offset += int64(len(line))
		if e, ok := parseAccessLine(line); ok {
			tr.add(e, now)
		}
	}
	if offset > maxAccessLog {
		if err := os.Truncate(path, 0); err == nil {
			offset = 0
		}
	}
	return offset
}

func (m *Manager) ban(ctx context.Context, uuid string, ips []string, now time.Time) {
	m.mu.Lock()
	if end, ok := m.banned[uuid]; ok && now.Before(end) {
		m.mu.Unlock()
		return
	}
	m.banned[uuid] = now.Add(m.cfg.XrayBan)
	cb := m.onLimit
	m.mu.Unlock()
	if err := m.removeUser(ctx, uuid); err != nil {
		m.log.Warn("xray limiter: cannot cut link", "uuid", uuid, "error", err)
	}
	m.log.Warn("xray limiter: link used from too many places, cut temporarily",
		"uuid", uuid, "ips", strings.Join(ips, ","), "until", now.Add(m.cfg.XrayBan).Format(time.RFC3339))
	if cb != nil {
		cb(uuid, ips)
	}
}

// unbanExpired puts links back once their ban ended — unless the device
// was deleted or disabled meanwhile (the active-device list decides).
func (m *Manager) unbanExpired(ctx context.Context, now time.Time) {
	m.mu.Lock()
	var due []string
	for uuid, end := range m.banned {
		if !now.Before(end) {
			due = append(due, uuid)
			delete(m.banned, uuid)
		}
	}
	m.mu.Unlock()
	if len(due) == 0 {
		return
	}
	active, err := m.activeUUIDs()
	if err != nil {
		m.log.Warn("xray limiter: cannot list devices", "error", err)
		return
	}
	for _, uuid := range due {
		if !active[uuid] {
			continue
		}
		if err := m.addUser(ctx, uuid); err != nil {
			m.log.Warn("xray limiter: cannot restore link", "uuid", uuid, "error", err)
		}
	}
}

func (m *Manager) activeUUIDs() (map[string]bool, error) {
	devices, err := m.store.ListActiveDevices(time.Now())
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, d := range devices {
		if d.Protocol == store.ProtocolXray {
			out[d.PublicKey] = true
		}
	}
	return out, nil
}
