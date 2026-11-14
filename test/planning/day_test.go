package planning_test

import (
	"testing"
	"time"

	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
)

func TestAmsterdamDay(t *testing.T) {
	for _, tc := range []struct{ instant, date, next string }{
		{"2026-11-04T23:00:00Z", "2026-11-05", "2026-11-05T23:00:00Z"},
		{"2026-11-04T22:59:59Z", "2026-11-04", "2026-11-04T23:00:00Z"},
		{"2026-03-28T23:00:00Z", "2026-03-29", "2026-03-29T22:00:00Z"}, // 23h day
		{"2026-10-24T22:00:00Z", "2026-10-25", "2026-10-25T23:00:00Z"}, // 25h day
		{"2026-10-25T00:30:00Z", "2026-10-25", "2026-10-25T23:00:00Z"},
		{"2026-10-25T01:30:00Z", "2026-10-25", "2026-10-25T23:00:00Z"}, // repeated local hour
		{"2028-02-28T23:00:00Z", "2028-02-29", "2028-02-29T23:00:00Z"},
		{"2026-12-31T23:00:00Z", "2027-01-01", "2027-01-01T23:00:00Z"},
		{"2026-07-01T00:30:00+09:00", "2026-06-30", "2026-06-30T22:00:00Z"},
	} {
		t.Run(tc.instant, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.instant)
			if err != nil {
				t.Fatal(err)
			}
			got := domainplanning.At(now)
			if got.PlanDate != tc.date || got.NextDayAt.Format(time.RFC3339) != tc.next || got.Timezone != domainplanning.Timezone || !got.ServerTime.Equal(now) {
				t.Fatalf("unexpected day: %+v", got)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	for _, raw := range []string{"0001-01-01", "9999-12-31", "2028-02-29"} {
		if _, err := domainplanning.ParseDate(raw); err != nil {
			t.Fatalf("valid date %s: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "0000-01-01", "10000-01-01", "2026-02-29", "2026-1-01", "2026-11-05T00:00:00Z", " 2026-11-05", "infinity"} {
		if _, err := domainplanning.ParseDate(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
