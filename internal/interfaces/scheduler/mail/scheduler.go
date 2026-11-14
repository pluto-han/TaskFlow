package interfacesschedulermail

import (
	"context"
	"time"

	applicationmail "github.com/pluto-han/taskflow/internal/application/mail"
)

type Scheduler struct {
	service  *applicationmail.Service
	observer applicationmail.Observer
}

func New(service *applicationmail.Service, observer applicationmail.Observer) *Scheduler {
	return &Scheduler{service: service, observer: observer}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll, cancel := context.WithTimeout(ctx, 8*time.Second)
			err := s.service.Poll(poll)
			cancel()
			if err != nil && ctx.Err() == nil {
				if s.observer != nil {
					s.observer.MailResult("error")
				}
			}
		}
	}
}
