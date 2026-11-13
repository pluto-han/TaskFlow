package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/pluto-han/taskflow/internal/httpapi"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	"github.com/pluto-han/taskflow/internal/session"
)

type ConfirmRequest struct {
	Token string `json:"token"`
}

func Mount(r *gin.Engine, s *Service, auth gin.HandlerFunc) {
	r.GET("/auth/email/verification", auth, s.statusHandler)
	// The disabled response is global, before authentication and body parsing.
	enabled := func(c *gin.Context) {
		if !s.Enabled() {
			httpapi.WriteError(c, 503, "EMAIL_DISABLED", "Email is not available")
			c.Abort()
			return
		}
		c.Next()
	}
	r.POST("/auth/password/reset/request", enabled, func(c *gin.Context) {
		var input ResetRequest
		if !readReset(c, &input) {
			return
		}
		if err := s.RequestReset(c.Request.Context(), input); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusAccepted)
	})
	r.POST("/auth/password/reset/confirm", enabled, func(c *gin.Context) {
		var input ResetConfirmRequest
		if !readReset(c, &input) {
			return
		}
		if err := s.ConfirmReset(c.Request.Context(), input); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	r.POST("/auth/email/verification", enabled, auth, func(c *gin.Context) {
		raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
		if err != nil {
			httpapi.WriteError(c, 413, "REQUEST_TOO_LARGE", "Request body too large")
			return
		}
		if raw = bytes.TrimSpace(raw); len(raw) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if raw[0] != '{' || decoder.Decode(new(struct{})) != nil || decoder.Decode(new(any)) != io.EOF {
				httpapi.WriteError(c, 400, "INVALID_REQUEST", "This request does not accept account fields")
				return
			}
		}
		p, _ := session.CurrentPrincipal(c.Request.Context())
		if err := s.Request(c.Request.Context(), p); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusAccepted)
	})
	r.POST("/auth/email/verification/confirm", enabled, func(c *gin.Context) {
		media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || media != "application/json" {
			httpapi.WriteError(c, 400, "INVALID_REQUEST", "Expected application/json")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
		if err != nil {
			httpapi.WriteError(c, 413, "REQUEST_TOO_LARGE", "Request body too large")
			return
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var input ConfirmRequest
		if !utf8.Valid(raw) || dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF {
			httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid verification body")
			return
		}
		if err = s.Confirm(c.Request.Context(), input.Token); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}
func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrResetEmail):
		httpapi.WriteError(c, 400, "INVALID_RESET_EMAIL", "Enter a valid email address")
	case errors.Is(err, ErrResetPassword):
		httpapi.WriteError(c, 400, "INVALID_RESET_PASSWORD", "Use 8–128 characters for the new password")
	case errors.Is(err, ErrResetToken):
		httpapi.WriteError(c, 400, "RESET_TOKEN_INVALID", "Reset link is invalid or expired")
	case errors.Is(err, infrapassword.ErrBusy):
		c.Header("Retry-After", "1")
		httpapi.WriteError(c, 503, "AUTH_BUSY", "Please retry later")
	case errors.Is(err, context.Canceled):
	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, 504, "REQUEST_TIMEOUT", "Request timed out")
	case errors.Is(err, ErrDisabled):
		httpapi.WriteError(c, 503, "EMAIL_DISABLED", "Email is not available")
	case errors.Is(err, ErrLimited):
		c.Header("Retry-After", "60")
		httpapi.WriteError(c, 429, "EMAIL_RATE_LIMITED", "Please retry later")
	case errors.Is(err, ErrToken):
		httpapi.WriteError(c, 400, "EMAIL_TOKEN_INVALID", "Verification link is invalid or expired")
	case errors.Is(err, session.ErrUnauthorized):
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
	default:
		httpapi.InternalError(c)
	}
}

// Reset requests never consume query-string tokens or ambient session credentials.
func readReset(c *gin.Context, input any) bool {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Expected application/json")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil {
		httpapi.WriteError(c, 413, "REQUEST_TOO_LARGE", "Request body too large")
		return false
	}
	raw = bytes.TrimSpace(raw)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if len(raw) == 0 || raw[0] != '{' || !utf8.Valid(raw) || dec.Decode(input) != nil || dec.Decode(new(any)) != io.EOF {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid reset body")
		return false
	}
	return true
}
