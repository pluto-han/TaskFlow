package mail_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	applicationmail "github.com/pluto-han/taskflow/internal/application/mail"
	inframail "github.com/pluto-han/taskflow/internal/infra/mail"
)

type resendTransport func(*http.Request) (*http.Response, error)

func (f resendTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func accountMessage(path string) applicationmail.Message {
	return applicationmail.Message{ID: "delivery-id", Recipient: "person@example.com", Link: "https://tasks.example.com/" + path + "#token=" + strings.Repeat("A", 43)}
}

func TestResendPayloadAndIdempotency(t *testing.T) {
	for _, path := range []string{"verify-email", "reset-password"} {
		t.Run(path, func(t *testing.T) {
			var calls int
			var first string
			transport := resendTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://api.resend.com/emails" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer re_test" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Idempotency-Key") != "taskflow-email/delivery-id" {
					t.Fatal("unexpected provider request contract")
				}
				body, _ := io.ReadAll(r.Body)
				if calls == 1 {
					first = string(body)
				} else if first != string(body) {
					t.Fatal("retry payload changed")
				}
				var payload struct {
					From, Subject, Text string
					To                  []string
				}
				if json.Unmarshal(body, &payload) != nil || payload.From != "TaskFlow <noreply@example.com>" || len(payload.To) != 1 || payload.To[0] != "person@example.com" || !strings.Contains(payload.Text, accountMessage(path).Link) || !strings.Contains(payload.Text, "ignore") {
					t.Fatal("invalid account message")
				}
				if path == "reset-password" && (payload.Subject != "Reset your TaskFlow password" || !strings.Contains(payload.Text, "30 minutes")) {
					t.Fatal("wrong reset template")
				}
				if path == "verify-email" && (payload.Subject != "Verify your TaskFlow email" || !strings.Contains(payload.Text, "24 hours")) {
					t.Fatal("wrong verification template")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"provider-id"}`))}, nil
			})
			sender, err := inframail.NewResendSender("re_test", "TaskFlow <noreply@example.com>", transport)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := sender.Send(t.Context(), accountMessage(path)); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 2 {
				t.Fatal("unexpected provider calls")
			}
		})
	}
}

func TestResendRejectsBadResponsesWithoutLeaking(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, "secret-payload"}, {"quota", 429, "secret-payload"}, {"server", 500, "secret-payload"},
		{"redirect", 307, "secret-payload"}, {"malformed", 200, "secret-payload"}, {"missing id", 200, `{}`},
		{"oversize", 200, strings.Repeat("x", 65537)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			sender, _ := inframail.NewResendSender("re_test", "noreply@example.com", resendTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {"https://other.example.com"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}))
			err := sender.Send(t.Context(), accountMessage("verify-email"))
			if err == nil || strings.Contains(err.Error(), "secret-payload") || calls != 1 {
				t.Fatal("unsafe provider failure handling")
			}
		})
	}
}

func TestResendCancellationAndTransportErrors(t *testing.T) {
	sender, _ := inframail.NewResendSender("re_test", "noreply@example.com", resendTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, errors.New("secret-payload")
	}))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := sender.Send(ctx, accountMessage("verify-email")); err == nil || strings.Contains(err.Error(), "secret-payload") {
		t.Fatal("unsafe cancellation")
	}
	ctx, cancel = context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := sender.Send(ctx, accountMessage("verify-email")); err == nil {
		t.Fatal("deadline ignored")
	}
}

func TestResendDoesNotFollowRedirect(t *testing.T) {
	visits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { visits++; w.WriteHeader(200) }))
	defer server.Close()
	sender, _ := inframail.NewResendSender("re_test", "noreply@example.com", resendTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.resend.com" {
			return http.DefaultTransport.RoundTrip(r)
		}
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": {server.URL}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	if sender.Send(t.Context(), accountMessage("verify-email")) == nil || visits != 0 {
		t.Fatal("provider redirect followed")
	}
}

func TestResendRejectsInvalidInputBeforeNetwork(t *testing.T) {
	for _, tc := range []struct{ key, from string }{{"", "noreply@example.com"}, {"re_key\nsecret", "noreply@example.com"}, {"re_test", "bad"}, {"re_test", "noreply@example.com\r\nBcc: other@example.com"}} {
		if _, err := inframail.NewResendSender(tc.key, tc.from, nil); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	sender, _ := inframail.NewResendSender("re_test", "noreply@example.com", resendTransport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input reached network"); return nil, nil }))
	for _, change := range []func(*applicationmail.Message){
		func(m *applicationmail.Message) { m.ID = "bad\nkey" }, func(m *applicationmail.Message) { m.Recipient = "a@example.com,b@example.com" },
		func(m *applicationmail.Message) {
			m.Link = "https://tasks.example.com/unknown#token=" + strings.Repeat("A", 43)
		},
		func(m *applicationmail.Message) { m.Link = "https://tasks.example.com/reset-password#token=invalid" },
		func(m *applicationmail.Message) { m.Link += "&token=duplicate" },
		func(m *applicationmail.Message) {
			m.Link = "http://public.example.com/reset-password#token=" + strings.Repeat("A", 43)
		},
	} {
		m := accountMessage("verify-email")
		change(&m)
		if sender.Send(t.Context(), m) == nil {
			t.Fatal("invalid message accepted")
		}
	}
}
