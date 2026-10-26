package user

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Register(c *gin.Context) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Expected application/json")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpapi.WriteError(c, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Request body too large")
		} else {
			httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		}
		return
	}
	var input RegisterRequest
	if !utf8.Valid(raw) || json.Unmarshal(raw, &input) != nil {
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}
	u, err := h.service.Register(c.Request.Context(), input)
	switch {
	case err == nil:
		c.JSON(http.StatusCreated, response(u))
	case errors.Is(err, ErrInvalidUser):
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_USER", "Invalid email or password")
	case errors.Is(err, ErrEmailExists):
		httpapi.WriteError(c, http.StatusConflict, "EMAIL_ALREADY_EXISTS", "Email already registered")
	case errors.Is(err, auth.ErrBusy):
		c.Header("Retry-After", "1")
		httpapi.WriteError(c, http.StatusServiceUnavailable, "REGISTRATION_BUSY", "Please retry later")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "Request timed out")
	default:
		httpapi.InternalError(c)
	}
}
