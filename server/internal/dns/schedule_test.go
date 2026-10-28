package dns

import (
	"testing"
	"time"
)

// monday is a fixed reference point: 2026-10-26 is a Monday.
var monday = time.Date(2026, time.October, 26, 0, 0, 0, 0, time.UTC)

func TestScheduleAllowed(t *testing.T) {
	tests := []struct {
		name  string
		sched Schedule
		at    time.Time
		want  bool
	}{
		{
			name:  "zero schedule is always allowed",
			sched: Schedule{},
			at:    monday.Add(3 * time.Hour),
			want:  true,
		},
		{
			name:  "start==end means around the clock",
			sched: Schedule{Start: "07:00", End: "07:00"},
			at:    monday.Add(23 * time.Hour),
			want:  true,
		},
		{
			name:  "start==end respects days",
			sched: Schedule{Days: []int{1}, Start: "07:00", End: "07:00"},
			at:    monday.Add(12 * time.Hour), // Monday
			want:  true,
		},
		{
			name:  "start==end on a wrong day",
			sched: Schedule{Days: []int{2}, Start: "07:00", End: "07:00"},
			at:    monday.Add(12 * time.Hour), // Monday, allowed only on Tuesday
			want:  false,
		},
		{
			name:  "inside the window",
			sched: Schedule{Start: "07:00", End: "22:00"},
			at:    monday.Add(12 * time.Hour),
			want:  true,
		},
		{
			name:  "before the window",
			sched: Schedule{Start: "07:00", End: "22:00"},
			at:    monday.Add(6 * time.Hour),
			want:  false,
		},
		{
			name:  "end boundary is exclusive",
			sched: Schedule{Start: "07:00", End: "22:00"},
			at:    monday.Add(22 * time.Hour),
			want:  false,
		},
		{
			name:  "start boundary is inclusive",
			sched: Schedule{Start: "07:00", End: "22:00"},
			at:    monday.Add(7 * time.Hour),
			want:  true,
		},
		{
			name:  "weekday restriction: allowed day",
			sched: Schedule{Days: []int{1, 2, 3, 4, 5}, Start: "07:00", End: "22:00"},
			at:    monday.Add(10 * time.Hour), // Monday
			want:  true,
		},
		{
			name:  "weekday restriction: weekend denied",
			sched: Schedule{Days: []int{1, 2, 3, 4, 5}, Start: "07:00", End: "22:00"},
			at:    monday.Add(6*24*time.Hour + 10*time.Hour), // Sunday
			want:  false,
		},
		{
			name:  "overnight window: evening part",
			sched: Schedule{Start: "22:00", End: "06:00"},
			at:    monday.Add(23 * time.Hour),
			want:  true,
		},
		{
			name:  "overnight window: morning after",
			sched: Schedule{Start: "22:00", End: "06:00"},
			at:    monday.Add(24*time.Hour + 5*time.Hour), // Tuesday 05:00
			want:  true,
		},
		{
			name:  "overnight window: midday denied",
			sched: Schedule{Start: "22:00", End: "06:00"},
			at:    monday.Add(12 * time.Hour),
			want:  false,
		},
		{
			name:  "overnight window belongs to previous active day",
			sched: Schedule{Days: []int{1}, Start: "22:00", End: "06:00"}, // Mondays only
			at:    monday.Add(24*time.Hour + 5*time.Hour),                 // Tue 05:00 is Monday night
			want:  true,
		},
		{
			name:  "overnight window: inactive previous day",
			sched: Schedule{Days: []int{2}, Start: "22:00", End: "06:00"}, // Tuesdays only
			at:    monday.Add(24*time.Hour + 5*time.Hour),                 // Tue morning needs Mon active
			want:  false,
		},
		{
			name:  "timezone shifts the window",
			sched: Schedule{Start: "07:00", End: "22:00", TZ: "Europe/Berlin"},
			at:    time.Date(2026, time.October, 26, 4, 30, 0, 0, time.UTC), // 05:30 Berlin
			want:  false,
		},
		{
			name:  "timezone allows what utc would deny",
			sched: Schedule{Start: "07:00", End: "22:00", TZ: "Europe/Berlin"}, // CET, UTC+1 on this date
			at:    time.Date(2026, time.October, 26, 6, 30, 0, 0, time.UTC),    // 07:30 Berlin, 06:30 UTC
			want:  true,
		},
		{
			name:  "bad timezone falls back to utc",
			sched: Schedule{Start: "07:00", End: "22:00", TZ: "Not/AZone"},
			at:    monday.Add(12 * time.Hour),
			want:  true,
		},
		{
			name:  "unparsable window never bricks dns",
			sched: Schedule{Start: "25:99", End: "ok?"},
			at:    monday.Add(12 * time.Hour),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sched.Allowed(tt.at); got != tt.want {
				t.Errorf("Allowed(%s) = %v, want %v", tt.at.Format("Mon 15:04"), got, tt.want)
			}
		})
	}
}
