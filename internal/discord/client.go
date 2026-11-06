package discord

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MessageSender has a fixed production destination; tests may supply a custom transport.
type MessageSender struct {
	token  string
	client *http.Client
}
type SendResult struct {
	Status     int
	RetryAfter time.Duration
	Global     bool
	Bucket     string
}

func NewMessageSender(token string, transport http.RoundTripper) (*MessageSender, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 4096 || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("invalid discord bot token")
	}
	return &MessageSender{token: token, client: &http.Client{Timeout: 5 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// CredentialFingerprint supports shared invalid-token cooldown without storing the token.
func (s *MessageSender) CredentialFingerprint() string {
	hash := sha256.Sum256([]byte(s.token))
	return hex.EncodeToString(hash[:16])
}
func (s *MessageSender) Send(ctx context.Context, channelID, nonce, title, userID string) (SendResult, error) {
	if !Snowflake(channelID) || !Snowflake(userID) || len(nonce) == 0 || len(nonce) > 25 {
		return SendResult{}, errors.New("invalid discord destination")
	}
	body, e := json.Marshal(map[string]any{"content": "<@" + userID + "> TaskFlow 今日任务：" + title, "nonce": nonce, "enforce_nonce": true, "allowed_mentions": map[string]any{"parse": []string{}, "users": []string{userID}}})
	if e != nil {
		return SendResult{}, errors.New("cannot encode discord message")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, "https://discord.com/api/v10/channels/"+channelID+"/messages", bytes.NewReader(body))
	if e != nil {
		return SendResult{}, errors.New("cannot construct discord request")
	}
	req.Header.Set("Authorization", "Bot "+s.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "TaskFlow/1.0")
	response, e := s.client.Do(req)
	if e != nil {
		return SendResult{}, errors.New("discord request failed")
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if e != nil || len(data) > 65536 {
		return SendResult{}, errors.New("invalid discord response")
	}
	result := SendResult{Status: response.StatusCode}
	if response.StatusCode >= 200 && response.StatusCode < 300 && response.Header.Get("X-RateLimit-Remaining") == "0" {
		seconds, err := strconv.ParseFloat(response.Header.Get("X-RateLimit-Reset-After"), 64)
		if err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds > 0 {
			if seconds > 9223372036 {
				seconds = 9223372036
			}
			result.RetryAfter = time.Duration(seconds * float64(time.Second))
		}
	}
	if response.StatusCode == http.StatusTooManyRequests {
		var payload struct {
			RetryAfter float64 `json:"retry_after"`
			Global     bool    `json:"global"`
		}
		_ = json.Unmarshal(data, &payload)
		seconds := payload.RetryAfter
		if header, e := strconv.ParseFloat(response.Header.Get("Retry-After"), 64); e == nil && header > seconds {
			seconds = header
		}
		if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
			seconds = 60
		}
		if seconds > 9223372036 {
			seconds = 9223372036
		}
		result.RetryAfter = time.Duration(seconds * float64(time.Second))
		result.Global = payload.Global || response.Header.Get("X-RateLimit-Global") == "true"
		bucket := response.Header.Get("X-RateLimit-Bucket")
		if len(bucket) <= 128 {
			result.Bucket = bucket
		}
	}
	return result, nil
}
