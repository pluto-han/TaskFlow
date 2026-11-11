package mail

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestCapabilitiesAndFixedOrigin(t *testing.T) {
	raw := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if h, e := tokenHash(raw); e != nil || len(h) != 32 {
		t.Fatal("canonical token rejected")
	}
	for _, v := range []string{"", raw + "=", raw[:42], strings.Repeat("!", 43), raw[:42] + "B"} {
		if _, e := tokenHash(v); e != ErrToken {
			t.Fatal("noncanonical token accepted")
		}
	}
	for _, origin := range []string{"https://example.com/path", "https://user@example.com", "https://example.com?query", "https://example.com#fragment", "http://example.com", "//example.com"} {
		if _, e := New(nil, &MemorySender{}, origin); e == nil {
			t.Fatal("untrusted base accepted")
		}
	}
	for _, origin := range []string{"https://taskflow.example.com", "http://127.0.0.1:5173"} {
		if _, e := New(nil, &MemorySender{}, origin); e != nil {
			t.Fatal("valid origin rejected")
		}
	}
	s, _ := New(nil, nil, "")
	if err := s.Confirm(context.Background(), raw); err != ErrDisabled {
		t.Fatal("disabled confirm queried database")
	}
}
func TestFakeInboxBoundAndCancellation(t *testing.T) {
	s := &MemorySender{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.Send(ctx, Message{}) == nil {
		t.Fatal("ignored cancellation")
	}
	for range 100 {
		if s.Send(context.Background(), Message{Recipient: "private@example.com", Link: "secret"}) != nil {
			t.Fatal("early limit")
		}
	}
	if s.Send(context.Background(), Message{}) == nil {
		t.Fatal("unbounded fake inbox")
	}
	if len(s.Take()) != 100 || len(s.Take()) != 0 {
		t.Fatal("fake did not drain")
	}
}
