package recurrence_test

import (
	"reflect"
	"testing"

	domainrecurrence "github.com/pluto-han/taskflow/internal/domain/recurrence"
)

func TestDates(t *testing.T) {
	for _, tc := range []struct {
		name, anchor, from string
		n                  int
		want               []string
	}{
		{"three", "2026-11-05", "2026-11-05", 3, []string{"2026-11-05", "2026-11-08", "2026-11-11"}},
		{"skip", "2026-11-05", "2026-11-09", 3, []string{"2026-11-11", "2026-11-14", "2026-11-17"}},
		{"spring DST", "2026-03-28", "2026-03-28", 1, []string{"2026-03-28", "2026-03-29", "2026-03-30"}},
		{"autumn DST", "2026-10-24", "2026-10-24", 1, []string{"2026-10-24", "2026-10-25", "2026-10-26"}},
		{"leap", "2028-02-28", "2028-02-28", 1, []string{"2028-02-28", "2028-02-29", "2028-03-01"}},
		{"year", "2026-12-30", "2026-12-01", 7, []string{"2026-12-30", "2027-01-06", "2027-01-13"}},
		{"far", "0001-01-01", "9999-12-30", 1, []string{"9999-12-30", "9999-12-31"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := domainrecurrence.Dates(tc.anchor, tc.n, tc.from, 3)
			if e != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("%v %v", got, e)
			}
		})
	}
	for _, n := range []int{-1, 0, 366} {
		if _, e := domainrecurrence.Dates("2026-11-05", n, "2026-11-05", 5); e != domainrecurrence.ErrInvalid {
			t.Fatal(n, e)
		}
	}
	for _, d := range []string{"2026-02-29", "2026-2-03", "infinity", "10000-01-01", ""} {
		if _, e := domainrecurrence.Dates(d, 1, "2026-11-05", 5); e != domainrecurrence.ErrInvalid {
			t.Fatal(d, e)
		}
	}
}
func TestReceiptAdvancesResume(t *testing.T) {
	next, e := domainrecurrence.NextAfter("2026-11-08", "2026-11-05", 3, "2026-11-08")
	if e != nil || next != "2026-11-11" {
		t.Fatal(next, e)
	}
	next, e = domainrecurrence.NextAfter("2026-11-09", "2026-11-05", 3, "2026-11-05")
	if e != nil || next != "2026-11-11" {
		t.Fatal(next, e)
	}
}
