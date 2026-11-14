package interfacesschedulerrecurrence

import (
	"context"
	"errors"
	"log/slog"
	"time"

	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
)

const ScanInterval = 30 * time.Second

const MaxBackoff = 5 * time.Minute

type ScheduleObserver interface {
	ObserveSchedule(bool, int, int, time.Duration)
}

// Scheduler owns one loop, never a goroutine per rule. A reset timer prevents
// overlapping scans and allows bounded retries without accumulating ticker events.
type Scheduler struct {
	runner   applicationrecurrence.BatchRunner
	logger   *slog.Logger
	observer ScheduleObserver
	now      func() time.Time
	wait     func(context.Context, time.Duration) bool
}

func NewScheduler(
	r applicationrecurrence.BatchRunner,
	logger *slog.Logger,
	observer ScheduleObserver,
) *Scheduler {
	return NewWithClock(r, logger, observer, time.Now, waitForScan)
}

// NewWithClock supplies the scheduler's business clock and cancellation-aware wait.
func NewWithClock(
	r applicationrecurrence.BatchRunner,
	logger *slog.Logger,
	observer ScheduleObserver,
	now func() time.Time,
	wait func(context.Context, time.Duration) bool,
) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{runner: r, logger: logger, observer: observer, now: now, wait: wait}
}

func waitForScan(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func retryDelay(previous time.Duration) time.Duration {
	if previous == 0 {
		return ScanInterval
	}

	next := previous * 2
	if next > MaxBackoff {
		return MaxBackoff
	}

	return next
}

func (s *Scheduler) Run(ctx context.Context) {
	backoff := time.Duration(0)
	for ctx.Err() == nil {
		started := time.Now()
		batchCtx, cancel := context.WithTimeout(ctx, applicationrecurrence.BatchTimeout)
		result, err := s.runner.GenerateBatch(batchCtx, s.now)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if s.observer != nil {
			s.observer.ObserveSchedule(err == nil, result.Processed, result.Generated, time.Since(started))
		}
		delay := ScanInterval
		switch {
		case errors.Is(err, applicationrecurrence.ErrDayChanged):
			delay = time.Second
		case err != nil:
			backoff = retryDelay(backoff)
			delay = backoff
			// Do not log SQL errors: PostgreSQL DETAIL can contain titles/credentials.
			s.logger.Warn("recurrence_scan_failed", "retry_seconds", delay.Seconds())
		default:
			backoff = 0
			if result.Processed == applicationrecurrence.BatchSize {
				delay = time.Second
			}
			if result.Processed > 0 {
				s.logger.Info("recurrence_scan_complete", "processed", result.Processed, "generated", result.Generated)
			}
		}
		if !s.wait(ctx, delay) {
			return
		}
	}
}
