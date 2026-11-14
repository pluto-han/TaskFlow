package discord_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	infradiscord "github.com/pluto-han/taskflow/internal/infra/discord"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestMessageSender(t *testing.T) {
	calls := 0
	tr := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://discord.com/api/v10/channels/123/messages" || r.Header.Get("Authorization") != "Bot secret" {
			t.Fatal("wrong destination")
		}
		var body struct {
			Content string `json:"content"`
			Nonce   string `json:"nonce"`
			Enforce bool   `json:"enforce_nonce"`
			Allowed struct {
				Parse []string `json:"parse"`
				Users []string `json:"users"`
			} `json:"allowed_mentions"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || !body.Enforce || body.Nonce != "nonce123" || body.Allowed.Parse == nil || len(body.Allowed.Parse) != 0 || len(body.Allowed.Users) != 1 || body.Allowed.Users[0] != "456" || body.Content != "<@456> TaskFlow 今日任务：@everyone <@789> <@&987>" {
			t.Fatal("unsafe message")
		}
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"2.5"}, "X-Ratelimit-Global": []string{"true"}}, Body: io.NopCloser(strings.NewReader(`{"retry_after":1.5,"global":true}`))}, nil
	})
	sender, e := infradiscord.NewMessageSender("secret", tr)
	if e != nil {
		t.Fatal(e)
	}
	result, e := sender.Send(context.Background(), "123", "nonce123", "@everyone <@789> <@&987>", "456")
	if e != nil || result.Status != 429 || result.RetryAfter != 2500*time.Millisecond || !result.Global {
		t.Fatal(result, e)
	}
	if _, e = sender.Send(context.Background(), "123/../evil", "nonce123", "x", "456"); e == nil || calls != 1 {
		t.Fatal("invalid destination accepted")
	}
	if _, e = sender.Send(context.Background(), "123", "nonce123", "x", "456><@everyone"); e == nil || calls != 1 {
		t.Fatal("invalid user accepted")
	}
	redirectCalls := 0
	redirect, _ := infradiscord.NewMessageSender("secret", transportFunc(func(r *http.Request) (*http.Response, error) {
		redirectCalls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://other.example/secret"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}))
	if result, err := redirect.Send(context.Background(), "123", "nonce", "title", "456"); err != nil || result.Status != 302 || redirectCalls != 1 {
		t.Fatal("redirect followed", err, redirectCalls)
	}
}

func TestSenderRateHeadersAndSafeErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		headers http.Header
		body    string
		retry   time.Duration
		global  bool
		wantErr bool
	}{
		{"success bucket", 200, http.Header{"X-Ratelimit-Remaining": []string{"0"}, "X-Ratelimit-Reset-After": []string{"1.5"}}, "{}", 1500 * time.Millisecond, false, false},
		{"global 429", 429, http.Header{}, `{"retry_after":3.25,"global":true}`, 3250 * time.Millisecond, true, false},
		{"oversize", 200, http.Header{}, strings.Repeat("a", 65537), 0, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := transportFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: tc.headers, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			sender, _ := infradiscord.NewMessageSender("private-token", tr)
			result, err := sender.Send(context.Background(), "123", "nonce", "title", "456")
			if (err != nil) != tc.wantErr || result.RetryAfter != tc.retry || result.Global != tc.global {
				t.Fatal(result, err)
			}
			if err != nil && strings.Contains(err.Error(), "private-token") {
				t.Fatal("credential leaked")
			}
		})
	}
	sender, _ := infradiscord.NewMessageSender("private-token", transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private-token") }))
	_, err := sender.Send(context.Background(), "123", "nonce", "title", "456")
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("transport error leaked")
	}
}

func TestCredentialFingerprintStableAndChanges(t *testing.T) {
	a, e := infradiscord.NewMessageSender("private-token", nil)
	if e != nil {
		t.Fatal(e)
	}
	same, _ := infradiscord.NewMessageSender("private-token", nil)
	other, _ := infradiscord.NewMessageSender("new-private-token", nil)
	if a.CredentialFingerprint() != same.CredentialFingerprint() || a.CredentialFingerprint() == other.CredentialFingerprint() || strings.Contains(a.CredentialFingerprint(), "private-token") {
		t.Fatal("invalid fingerprint")
	}
}
