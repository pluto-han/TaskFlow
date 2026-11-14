package applicationnotification

import (
	"time"
)

type SendResult struct {
	Status     int
	RetryAfter time.Duration
	Global     bool
	Bucket     string
}
