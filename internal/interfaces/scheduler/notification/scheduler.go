package interfacesschedulernotification

import (
	"context"
	"log/slog"
	"time"

	applicationnotification "github.com/pluto-han/taskflow/internal/application/notification"
)

type Scheduler struct {
	service  *applicationnotification.Service
	logger   *slog.Logger
	observer applicationnotification.Observer
}

func New(
	service *applicationnotification.Service,
	logger *slog.Logger,
	observer applicationnotification.Observer,
) *Scheduler {
	return &Scheduler{service: service, logger: logger, observer: observer}
}

func (s *Scheduler) Run(ctx context.Context) {
	delay := time.Duration(0)
	for ctx.Err() == nil {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		batch, cancel := context.WithTimeout(ctx, 10*time.Second)
		found, e := s.service.ProcessOne(batch)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if s.observer != nil {
			s.observer.ObserveNotificationPoll(e == nil)
		}
		if e != nil {
			s.logger.Warn("notification_worker_failed")
			delay = 30 * time.Second
		} else if found {
			delay = time.Second
		} else {
			delay = 5 * time.Second
		}
	}
}
