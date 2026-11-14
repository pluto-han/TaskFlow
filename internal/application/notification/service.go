// Package applicationnotification processes transactional outbox deliveries.
package applicationnotification

import (
	"context"
	"crypto/rand"
	"errors"
	"time"
)

const maxAttempts = 8

type Event struct {
	ID            string
	RuleID        string
	TaskID        string
	ScheduledDate string
	Title         string
	Status        string
	Attempts      int
	LastError     string
	CreatedAt     time.Time
}

type Claim struct {
	Event
	Token, Channel, MentionUser string
	Reclaimed                   bool
}

type Observer interface {
	ObserveNotificationPoll(bool)
	ObserveNotificationSend(int, bool)
	ObserveNotificationReclaim()
}

type Sender interface {
	Send(context.Context, string, string, string, string) (SendResult, error)
}

type Service struct {
	repo       Repository
	sender     Sender
	credential string
	observer   Observer
	now        func() time.Time
}

// WithObserver must be called before the worker starts.
func (s *Service) WithObserver(observer Observer) *Service {
	s.observer = observer
	return s
}

func New(repo Repository, sender Sender) *Service { return NewWithClock(repo, sender, time.Now) }

func NewWithClock(repo Repository, sender Sender, now func() time.Time) *Service {
	credential := "default"
	if item, ok := sender.(interface{ CredentialFingerprint() string }); ok {
		credential = item.CredentialFingerprint()
	}
	return &Service{repo: repo, sender: sender, credential: credential, now: now}
}

func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	base := time.Duration(1<<uint(attempt-1)) * 15 * time.Second
	var b [1]byte
	if _, e := rand.Read(b[:]); e != nil {
		return base
	}

	return base + time.Duration(b[0])*base/1024
}

func (s *Service) ProcessOne(ctx context.Context) (bool, error) {
	if s.sender == nil {
		return false, errors.New("notification sender not configured")
	}

	c, e := s.repo.Claim(ctx)
	if e != nil || c == nil {
		return false, e
	}
	if c.Reclaimed && s.observer != nil {
		s.observer.ObserveNotificationReclaim()
	}
	valid, e := s.repo.Preflight(ctx, c)
	if e != nil || !valid {
		return true, e
	}

	nonce := c.ID[:8] + c.ID[9:13] + c.ID[14:18] + c.ID[19:23] + c.ID[24:28] // stable and at most 25 characters
	result, sendErr := s.sender.Send(ctx, c.Channel, nonce, c.Title, c.MentionUser)
	if s.observer != nil {
		s.observer.ObserveNotificationSend(result.Status, sendErr != nil)
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	if e = s.repo.Finish(ctx, c, delivery(c, result, sendErr, s.credential, s.now)); e != nil {
		return true, e
	}

	return true, nil
}

func delivery(
	c *Claim,
	result SendResult,
	sendErr error,
	credential string,
	now func() time.Time,
) Delivery {
	status, reason := "sent", ""
	delay := time.Duration(0)
	switch {
	case sendErr != nil:
		status, reason, delay = "retrying", "network", backoff(c.Attempts)
	case result.Status >= 200 && result.Status < 300:
	case result.Status == 429:
		status, reason, delay = "retrying", "rate_limited", result.RetryAfter
	case result.Status == 401:
		status, reason = "failed", "discord_token_invalid"
	case result.Status == 403:
		status, reason = "failed", "discord_permission"
	case result.Status == 404:
		status, reason = "failed", "discord_channel_missing"
	case result.Status >= 500:
		status, reason, delay = "retrying", "discord_unavailable", backoff(c.Attempts)
	default:
		status, reason = "failed", "discord_rejected"
	}
	if status == "retrying" && c.Attempts >= maxAttempts {
		status, reason = "failed", "attempts_exhausted"
	}
	out := Delivery{Status: status, Reason: reason, Delay: delay}
	if sendErr == nil && (result.Status == 429 || result.Status == 401 || result.Status == 403 || result.Status == 404 || (result.Status >= 200 && result.Status < 300 && result.RetryAfter > 0)) {
		scope := "channel:" + c.Channel
		until := now().Add(delay)
		if result.Status >= 200 && result.Status < 300 {
			until = now().Add(result.RetryAfter)
		}
		if result.Global {
			scope = "global"
		}
		if result.Status == 401 {
			scope = "credential:" + credential
			until = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
		}
		if result.Status == 403 || result.Status == 404 {
			scope = "channel:" + c.Channel
			until = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
		}
		out.CooldownScope = scope
		out.CooldownUntil = until
	}
	return out
}

func (s *Service) List(ctx context.Context, owner string) ([]Event, error) {
	return s.repo.List(ctx, owner)
}

type Repository interface {
	List(context.Context, string) ([]Event, error)
	Claim(context.Context) (*Claim, error)
	Preflight(context.Context, *Claim) (bool, error)
	Finish(context.Context, *Claim, Delivery) error
}

type Delivery struct {
	Status        string
	Reason        string
	Delay         time.Duration
	CooldownScope string
	CooldownUntil time.Time
}
