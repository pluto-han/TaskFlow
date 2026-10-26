package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/auth"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrUnauthorized):
		c.Header("WWW-Authenticate", "Bearer")
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(err, auth.ErrBusy):
		c.Header("Retry-After", "1")
		httpapi.WriteError(c, 503, "AUTH_BUSY", "Please retry later")
	case errors.Is(err, context.Canceled):
	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, 504, "REQUEST_TIMEOUT", "Request timed out")
	default:
		httpapi.InternalError(c)
	}
}

func (h *Handler) Login(c *gin.Context) {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Expected application/json")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			httpapi.WriteError(c, 413, "REQUEST_TOO_LARGE", "Request body too large")
		} else {
			httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		}
		return
	}
	var input LoginRequest
	if !utf8.Valid(raw) || json.Unmarshal(raw, &input) != nil {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		return
	}
	result, err := h.service.Login(c.Request.Context(), input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, result)
}

func (h *Handler) RequireSession(c *gin.Context) {
	headers := c.Request.Header.Values("Authorization")
	if len(headers) != 1 {
		writeError(c, ErrUnauthorized)
		c.Abort()
		return
	}
	fields := strings.Fields(headers[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		writeError(c, ErrUnauthorized)
		c.Abort()
		return
	}
	p, err := h.service.Authenticate(c.Request.Context(), fields[1])
	if err != nil {
		writeError(c, err)
		c.Abort()
		return
	}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), principalKey{}, p))
	c.Next()
}
func (h *Handler) Me(c *gin.Context) {
	p, ok := CurrentPrincipal(c.Request.Context())
	if !ok {
		writeError(c, ErrUnauthorized)
		return
	}
	result, err := h.service.Me(c.Request.Context(), p)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, result)
}
func (h *Handler) Logout(c *gin.Context) {
	p, ok := CurrentPrincipal(c.Request.Context())
	if !ok {
		writeError(c, ErrUnauthorized)
		return
	}
	if err := h.service.Logout(c.Request.Context(), p); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Mount is shared by main and integration tests so route protection is tested.
func Mount(router *gin.Engine, service *Service) {
	h := NewHandler(service)
	group := router.Group("/auth")
	group.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Header("Pragma", "no-cache"); c.Next() })
	group.POST("/login", NewLoginLimiter().Middleware, h.Login)
	group.GET("/me", h.RequireSession, h.Me)
	group.POST("/logout", h.RequireSession, h.Logout)
}
