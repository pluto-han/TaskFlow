package config

import "errors"

// Explicit opt-in: misspelled values must not silently disable or enable writes.
func (c Config) RecurrenceEnabled() (bool, error) {
	switch c.RecurrenceScheduler {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("TASKFLOW_RECURRENCE_ENABLED must be true or false")
	}
}
