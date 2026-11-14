package mail_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	applicationmail "github.com/pluto-han/taskflow/internal/application/mail"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	inframail "github.com/pluto-han/taskflow/internal/infra/mail"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
)

func TestCapabilitiesAndFixedOrigin(t *testing.T) {
	raw := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	repo := &tokenRepository{}
	service, _ := applicationaccount.NewCapabilities(repo, true, "https://taskflow.example.com", nil)
	if e := service.Confirm(context.Background(), raw); e != nil || len(repo.hash) != 32 {
		t.Fatal("canonical token rejected")
	}
	for _, v := range []string{"", raw + "=", raw[:42], strings.Repeat("!", 43), raw[:42] + "B"} {
		if e := service.Confirm(context.Background(), v); e != domainaccount.ErrToken {
			t.Fatal("noncanonical token accepted")
		}
	}
	for _, origin := range []string{"https://example.com/path", "https://user@example.com", "https://example.com?query", "https://example.com#fragment", "http://example.com", "//example.com"} {
		if _, e := applicationaccount.NewCapabilities(nil, &inframail.MemorySender{} != nil, origin, infrapassword.PasswordHasher{}); e == nil {
			t.Fatal("untrusted base accepted")
		}
	}
	for _, origin := range []string{"https://taskflow.example.com", "http://127.0.0.1:5173"} {
		if _, e := applicationaccount.NewCapabilities(nil, &inframail.MemorySender{} != nil, origin, infrapassword.PasswordHasher{}); e != nil {
			t.Fatal("valid origin rejected")
		}
	}
	s, _ := applicationaccount.NewCapabilities(nil, false, "", infrapassword.PasswordHasher{})
	if err := s.Confirm(context.Background(), raw); err != domainaccount.ErrDisabled {
		t.Fatal("disabled confirm queried database")
	}
}
func TestFakeInboxBoundAndCancellation(t *testing.T) {
	s := &inframail.MemorySender{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Send(ctx, applicationmail.Message{}) == nil {
		t.Fatal("ignored cancellation")
	}
	for range 100 {
		if s.Send(context.Background(), applicationmail.Message{Recipient: "private@example.com", Link: "secret"}) != nil {
			t.Fatal("early limit")
		}
	}
	if s.Send(context.Background(), applicationmail.Message{}) == nil {
		t.Fatal("unbounded fake inbox")
	}
	if len(s.Take()) != 100 || len(s.Take()) != 0 {
		t.Fatal("fake did not drain")
	}
}

type tokenRepository struct {
	applicationaccount.CapabilityRepository
	hash []byte
}

func (r *tokenRepository) Confirm(_ context.Context, hash []byte) error { r.hash = hash; return nil }
