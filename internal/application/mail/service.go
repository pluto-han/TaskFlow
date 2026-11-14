// Package applicationmail coordinates transactional email outbox delivery.
package applicationmail

import (
	"context"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

// Sender must honor cancellation and must never log messages or credentials.
// Delivery is at least once: a crash after sending can cause duplicate mail.
type Sender interface {
	Send(context.Context, Message) error
}

type Message struct{ ID, Recipient, Link string }

type Claim struct {
	Message  Message
	Lease    string
	Attempts int
}

type Repository interface {
	Claim(context.Context) (*Claim, error)
	Finish(context.Context, *Claim, bool) error
}

type Observer interface{ MailResult(string) }

type Service struct {
	repo     Repository
	sender   Sender
	observer Observer
}

func New(repo Repository, sender Sender) *Service { return &Service{repo: repo, sender: sender} }

func (s *Service) Enabled() bool { return s != nil && s.sender != nil }

func (s *Service) WithObserver(o Observer) *Service {
	s.observer = o
	return s
}

func (s *Service) observe(outcome string) {
	if s.observer != nil {
		s.observer.MailResult(outcome)
	}
}

// Poll claims at most one message and sends outside the database transaction.
// Acknowledgment is fenced by the lease; raw sender errors are never persisted.
func (s *Service) Poll(ctx context.Context) error {
	if !s.Enabled() {
		return domainaccount.ErrDisabled
	}

	claim, err := s.repo.Claim(ctx)
	if err != nil || claim == nil {
		return err
	}

	m, attempts := claim.Message, claim.Attempts

	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	sendErr := s.sender.Send(sendCtx, m)
	cancel()
	outcome := "success"
	if sendErr != nil {
		outcome = "retry"
		if attempts >= 5 {
			outcome = "failed"
		}
	}
	s.observe(outcome)

	// No network error text/body enters persistence or logs.
	return s.repo.Finish(ctx, claim, sendErr == nil)
}
