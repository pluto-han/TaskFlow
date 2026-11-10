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

type Handler struct {
	service       *Service
	browserOrigin string
	secureCookie  bool
}

func NewHandler(service *Service, options ...httpapi.SecurityOptions) *Handler {
	security := httpapi.SecurityOptions{}
	if len(options) > 0 {
		security = options[0]
	}
	origin := security.PublicOrigin
	if origin == "" && !security.Production {
		origin = "http://127.0.0.1:5173"
	}
	return &Handler{service: service, browserOrigin: origin,
		secureCookie: security.Production || strings.HasPrefix(strings.ToLower(origin), "https://")}
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidSessionQuery):
		httpapi.WriteError(c, 400, "INVALID_QUERY", "Invalid session query or ID")
	case errors.Is(err, ErrSessionNotFound):
		httpapi.WriteError(c, 404, "SESSION_NOT_FOUND", "Session not found")
	case errors.Is(err, ErrInvalidPasswordChange):
		httpapi.WriteError(c, 400, "INVALID_PASSWORD_CHANGE", "Check the current password and use 8–128 characters for the new password")
	case errors.Is(err, ErrCurrentPassword):
		httpapi.WriteError(c, 400, "CURRENT_PASSWORD_INCORRECT", "The current password is incorrect")
	case errors.Is(err, ErrCredentialsChanged):
		httpapi.WriteError(c, 409, "CREDENTIALS_CHANGED", "Credentials have changed; sign in again")
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

func readLogin(c *gin.Context) (LoginRequest, bool) {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Expected application/json")
		return LoginRequest{}, false
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
		return LoginRequest{}, false
	}
	var input LoginRequest
	if !utf8.Valid(raw) || json.Unmarshal(raw, &input) != nil {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		return LoginRequest{}, false
	}
	return input, true
}

func (h *Handler) Login(c *gin.Context) {
	if !h.bearerOnly(c) {
		return
	}
	input, ok := readLogin(c)
	if !ok {
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
	cookie, present, valid := h.sessionCookie(c.Request)
	headers := c.Request.Header.Values("Authorization")
	if len(headers) > 0 && present {
		ambiguousCredentials(c)
		return
	}
	if present {
		if !valid {
			h.rejectSession(c, ErrUnauthorized)
			return
		}
		if !h.browserOriginAllowed(c, isMutation(c.Request.Method)) {
			return
		}
		p, err := h.service.AuthenticateBrowser(c.Request.Context(), cookie)
		if err != nil {
			h.rejectSession(c, err)
			return
		}
		if isMutation(c.Request.Method) && !validCSRF(c, p.CSRFSecret) {
			return
		}
		ctx := context.WithValue(c.Request.Context(), browserPrincipalKey{}, p)
		c.Request = c.Request.WithContext(context.WithValue(ctx, principalKey{}, p.Principal))
		c.Next()
		return
	}
	if len(headers) != 1 {
		h.rejectSession(c, ErrUnauthorized)
		return
	}
	fields := strings.Fields(headers[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		h.rejectSession(c, ErrUnauthorized)
		return
	}
	p, err := h.service.Authenticate(c.Request.Context(), fields[1])
	if err != nil {
		h.rejectSession(c, err)
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
func Mount(router *gin.Engine, service *Service, options ...httpapi.SecurityOptions) *Handler {
	h := NewHandler(service, options...)
	group := router.Group("/auth")
	group.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Header("Pragma", "no-cache"); c.Next() })
	limiter := NewLoginLimiter()
	group.POST("/login", limiter.Middleware, h.Login)
	group.GET("/me", h.requireBearer, h.Me)
	group.POST("/logout", h.requireBearer, h.Logout)
	group.POST("/browser/login", limiter.Middleware, h.BrowserLogin)
	group.GET("/browser/session", h.requireBrowser, h.BrowserSession)
	group.POST("/browser/logout", h.requireBrowser, h.BrowserLogout)
	group.GET("/sessions", h.RequireSession, h.ListSessions)
	group.DELETE("/sessions/:id", h.RequireSession, h.RevokeSession)
	group.POST("/password/change", h.RequireSession, h.ChangePassword)
	return h
}
