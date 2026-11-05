// Package planning defines TaskFlow's business calendar independently of host TZ.
package planning

import (
	"errors"
	"time"
	_ "time/tzdata" // scratch images have no system zoneinfo database.
)

const Timezone = "Europe/Amsterdam"
const DateLayout = "2006-01-02"

var location = mustLocation()

func mustLocation() *time.Location {
	loc, err := time.LoadLocation(Timezone)
	if err != nil {
		panic("embedded Amsterdam timezone unavailable")
	}
	return loc
}

// ParseDate validates a date, not an instant. UTC here is only a date carrier.
func ParseDate(raw string) (time.Time, error) {
	d, err := time.Parse(DateLayout, raw)
	if err != nil || len(raw) != 10 || d.Year() < 1 || d.Year() > 9999 {
		return time.Time{}, errors.New("invalid plan date")
	}
	return d, nil
}

type Day struct {
	PlanDate   string    `json:"plan_date"`
	Timezone   string    `json:"timezone"`
	ServerTime time.Time `json:"server_time"`
	NextDayAt  time.Time `json:"next_day_at"`
}

func At(now time.Time) Day {
	local := now.In(location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return Day{PlanDate: local.Format(DateLayout), Timezone: Timezone,
		ServerTime: now.UTC(), NextDayAt: midnight.AddDate(0, 0, 1).UTC()}
}
