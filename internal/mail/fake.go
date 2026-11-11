package mail

import (
	"context"
	"errors"
	"sync"
)

// MemorySender is a bounded fake inbox for development and tests. It has no HTTP
// endpoint and never sends external mail. Take removes sensitive materials.
type MemorySender struct {
	mu       sync.Mutex
	messages []Message
}

func (s *MemorySender) Send(ctx context.Context, m Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) >= 100 {
		return errors.New("fake inbox full")
	}
	s.messages = append(s.messages, m)
	return nil
}
func (s *MemorySender) Take() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.messages
	s.messages = nil
	return m
}
