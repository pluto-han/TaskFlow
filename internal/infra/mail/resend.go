package inframail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strings"
	"time"

	applicationmail "github.com/pluto-han/taskflow/internal/application/mail"
)

// ResendSender uses a fixed HTTPS destination. A custom transport is for tests,
// not a configurable provider URL. The Outbox owns retry scheduling.
type ResendSender struct {
	key, from string
	client    *http.Client
}

func NewResendSender(key, from string, transport http.RoundTripper) (*ResendSender, error) {
	if err := ValidateResend(key, from); err != nil {
		return nil, err
	}

	return &ResendSender{key: key, from: from, client: &http.Client{
		Timeout: 5 * time.Second, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (s *ResendSender) Send(ctx context.Context, m applicationmail.Message) error {
	// Purpose comes from the stored, application-generated link. Never turn an
	// arbitrary URL or a malformed capability into an account email.
	u, err := url.Parse(m.Link)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		return errors.New("invalid account email")
	}

	fragment, err := url.ParseQuery(u.Fragment)
	token := fragment.Get("token")
	raw, decodeErr := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(fragment) != 1 || len(fragment["token"]) != 1 || decodeErr != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return errors.New("invalid account email")
	}
	if len(m.ID) == 0 || len(m.ID) > 128 || strings.ContainsAny(m.ID, " \t\r\n") || len(m.Recipient) > 320 || strings.ContainsAny(m.Recipient, "\r\n") {
		return errors.New("invalid account email")
	}

	address, err := netmail.ParseAddress(m.Recipient)
	if err != nil || address.Address != m.Recipient {
		return errors.New("invalid account email")
	}

	var subject, instruction, expiry string
	switch u.Path {
	case "/verify-email":
		subject, instruction, expiry = "Verify your TaskFlow email", "Confirm your email address using the link below.", "24 hours"
	case "/reset-password":
		subject, instruction, expiry = "Reset your TaskFlow password", "Choose a new password using the link below.", "30 minutes"
	default:
		return errors.New("invalid account email")
	}
	body, err := json.Marshal(struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}{s.from, []string{m.Recipient}, subject,
		instruction + "\n\n" + m.Link + "\n\nThis link expires " + expiry + " after it was requested and can only be used once.\nIf you did not request this email, you can ignore it.\n\nTaskFlow"})
	if err != nil {
		return errors.New("cannot encode account email")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return errors.New("cannot construct mail request")
	}

	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "taskflow-email/"+m.ID)
	response, err := s.client.Do(req)
	if err != nil {
		// net/http errors may include URLs or transport-provided secret material.
		return errors.New("mail provider request failed")
	}

	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("mail provider rejected request")
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	var result struct {
		ID string `json:"id"`
	}
	if err != nil || len(data) > 65536 || json.Unmarshal(data, &result) != nil || result.ID == "" {
		return errors.New("invalid mail provider response")
	}

	return nil
}

func ValidateResend(key, from string) error {
	if len(key) < 4 || len(key) > 4096 || !strings.HasPrefix(key, "re_") || strings.ContainsAny(key, " \t\r\n") {
		return errors.New("invalid Resend API key")
	}
	if len(from) > 320 || strings.ContainsAny(from, "\r\n") {
		return errors.New("invalid mail sender address")
	}

	address, err := netmail.ParseAddress(from)
	if err != nil || !strings.Contains(address.Address, "@") {
		return errors.New("invalid mail sender address")
	}

	return nil
}
