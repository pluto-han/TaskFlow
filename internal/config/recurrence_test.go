package config

import "testing"

func TestRecurrenceFlag(t *testing.T) {
	for _, tc := range []struct {
		raw              string
		enabled, invalid bool
	}{{"", false, false}, {"false", false, false}, {"true", true, false}, {"TRUE", false, true}, {"1", false, true}, {"typo", false, true}} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("TASKFLOW_RECURRENCE_ENABLED", tc.raw)
			got, err := Load().RecurrenceEnabled()
			if got != tc.enabled || (err != nil) != tc.invalid {
				t.Fatal(got, err)
			}
		})
	}
}
