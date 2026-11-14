package interfaceshttpaccount

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
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
)

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domainaccount.ErrResetEmail):
		httpapi.WriteError(c, 400, "INVALID_RESET_EMAIL", "Enter a valid email address")
	case errors.Is(err, domainaccount.ErrResetPassword):
		httpapi.WriteError(c, 400, "INVALID_RESET_PASSWORD", "Use 8–128 characters for the new password")
	case errors.Is(err, domainaccount.ErrResetToken):
		httpapi.WriteError(c, 400, "RESET_TOKEN_INVALID", "Reset link is invalid or expired")
	case errors.Is(err, applicationaccount.ErrPasswordBusy):
		c.Header("Retry-After", "1")
		httpapi.WriteError(c, 503, "AUTH_BUSY", "Please retry later")
	case errors.Is(err, context.Canceled):
	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, 504, "REQUEST_TIMEOUT", "Request timed out")
	case errors.Is(err, domainaccount.ErrDisabled):
		httpapi.WriteError(c, 503, "EMAIL_DISABLED", "Email is not available")
	case errors.Is(err, domainaccount.ErrLimited):
		c.Header("Retry-After", "60")
		httpapi.WriteError(c, 429, "EMAIL_RATE_LIMITED", "Please retry later")
	case errors.Is(err, domainaccount.ErrToken):
		httpapi.WriteError(c, 400, "EMAIL_TOKEN_INVALID", "Verification link is invalid or expired")
	case errors.Is(err, domainaccount.ErrUnauthorized):
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

type CapabilityHandler struct {
	service *applicationaccount.CapabilityService
}

func (h *CapabilityHandler) Status(c *gin.Context) {
	p, _ := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	result, err := h.service.Status(c.Request.Context(), p)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(200, verificationStatusResponseFromResult(result))
}

func NewCapabilities(service *applicationaccount.CapabilityService) *CapabilityHandler {
	return &CapabilityHandler{service: service}
}

func (h *CapabilityHandler) RequestReset(c *gin.Context) {
	var input ResetRequest
	if !readReset(c, &input) {
		return
	}
	if err := h.service.RequestReset(c.Request.Context(), input.input()); err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusAccepted)
}

func (h *CapabilityHandler) ConfirmReset(c *gin.Context) {
	var input ResetConfirmRequest
	if !readReset(c, &input) {
		return
	}
	if err := h.service.ConfirmReset(c.Request.Context(), input.input()); err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *CapabilityHandler) Request(c *gin.Context) {
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
	p, _ := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if err := h.service.Request(c.Request.Context(), p); err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusAccepted)
}

func (h *CapabilityHandler) Confirm(c *gin.Context) {
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
	if err = h.service.Confirm(c.Request.Context(), input.Token); err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *CapabilityHandler) RequireEnabled(c *gin.Context) {
	if !h.service.Enabled() {
		httpapi.WriteError(c, 503, "EMAIL_DISABLED", "Email is not available")
		c.Abort()
		return
	}

	c.Next()
}

func (v ResetRequest) input() applicationaccount.ResetInput {
	return applicationaccount.ResetInput{
		Email: v.Email,
	}
}

func (v ResetConfirmRequest) input() applicationaccount.ResetConfirmInput {
	return applicationaccount.ResetConfirmInput{
		Token:       v.Token,
		NewPassword: v.NewPassword,
	}
}

func verificationStatusResponseFromResult(v applicationaccount.VerificationResult) VerificationStatusResponse {
	return VerificationStatusResponse{
		Enabled:    v.Enabled,
		VerifiedAt: v.VerifiedAt,
	}
}
