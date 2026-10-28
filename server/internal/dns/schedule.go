package dns

import (
	"strconv"
	"strings"
	"time"
	// The runtime image (alpine) ships no tzdata, so the IANA database is
	// embedded into the binary — otherwise every schedule TZ silently falls
	// back to UTC inside the container.
	_ "time/tzdata"
)

// Allowed reports whether the DNS query may be answered at the given moment.
//
// A zero Schedule (no days, no window) is "no schedule": always allowed.
// Empty Days means every day. start == end means around the clock. Windows may
// cross midnight: 22:00–06:00 allows evenings of active days and mornings of
// the days that follow them.
func (s Schedule) Allowed(now time.Time) bool {
	startM, startOK := parseHHMM(s.Start)
	endM, endOK := parseHHMM(s.End)
	if !startOK || !endOK {
		// Missing or unparsable window — including the zero Schedule —
		// means "no schedule": never brick the client's DNS over config.
		return true
	}

	loc := time.UTC
	if s.TZ != "" {
		if l, err := time.LoadLocation(s.TZ); err == nil {
			loc = l
		}
	}
	local := now.In(loc)
	day := int(local.Weekday()) // 0=Sun..6=Sat -> 7=Sun..1=Mon
	if day == 0 {
		day = 7
	}
	minutes := local.Hour()*60 + local.Minute()

	active := func(d int) bool {
		if len(s.Days) == 0 {
			return true
		}
		for _, x := range s.Days {
			if x == d {
				return true
			}
		}
		return false
	}

	switch {
	case startM == endM:
		// Around the clock on active days.
		return active(day)
	case startM < endM:
		return active(day) && minutes >= startM && minutes < endM
	default:
		// Window crosses midnight: the evening half belongs to the current
		// day, the morning half to the day before it.
		prev := day - 1
		if prev == 0 {
			prev = 7
		}
		if active(day) && minutes >= startM {
			return true
		}
		return active(prev) && minutes < endM
	}
}

// parseHHMM parses "HH:MM" into minutes since midnight.
func parseHHMM(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	hh, mm, ok := strings.Cut(s, ":")
	if !ok {
		return 0, false
	}
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}
