package domainrecurrence

import (
	"time"

	domainplanning "github.com/pluto-han/taskflow/internal/domain/planning"
)

// Civil dates use UTC as a date carrier: subtraction cannot cross a DST gap.
func Dates(anchor string, interval int, from string, count int) ([]string, error) {
	a, e := domainplanning.ParseDate(anchor)
	if e != nil {
		return nil, ErrInvalid
	}

	start, e := domainplanning.ParseDate(from)
	if e != nil || interval < 1 || interval > 365 || count < 1 || count > 5 {
		return nil, ErrInvalid
	}
	if start.After(a) {
		// Unix-day difference avoids time.Duration overflow for years far apart.
		days := (start.Unix() - a.Unix()) / 86400
		steps := (days + int64(interval) - 1) / int64(interval)
		a = a.AddDate(0, 0, int(steps)*interval)
	}
	result := []string{}
	for range count {
		if a.Year() > 9999 {
			break
		}
		result = append(result, a.Format(domainplanning.DateLayout))
		a = a.AddDate(0, 0, interval)
	}
	if len(result) == 0 {
		return nil, ErrInvalid
	}

	return result, nil
}

func NextAfter(today, anchor string, interval int, last string) (string, error) {
	from := today
	if last != "" && last >= today {
		d, e := domainplanning.ParseDate(last)
		if e != nil {
			return "", ErrInvalid
		}

		from = d.AddDate(0, 0, 1).Format(time.DateOnly)
	}
	dates, e := Dates(anchor, interval, from, 1)
	if e != nil {
		return "", e
	}

	return dates[0], nil
}
