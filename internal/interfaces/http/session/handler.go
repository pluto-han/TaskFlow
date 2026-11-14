package interfaceshttpsession

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
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
)

type Handler struct {
	service       *applicationsession.Service
	browserOrigin string
	secureCookie  bool
}

func New(
	service *applicationsession.Service,
	options ...interfaceshttpmiddleware.SecurityOptions,
) *Handler {
	security := interfaceshttpmiddleware.SecurityOptions{}
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
	case errors.Is(err, domainsession.ErrInvalidSessionQuery):
		httpapi.WriteError(c, 400, "INVALID_QUERY", "Invalid session query or ID")
	case errors.Is(err, domainsession.ErrSessionNotFound):
		httpapi.WriteError(c, 404, "SESSION_NOT_FOUND", "Session not found")
	case errors.Is(err, domainsession.ErrInvalidPasswordChange):
		httpapi.WriteError(c, 400, "INVALID_PASSWORD_CHANGE", "Check the current password and use 8–128 characters for the new password")
	case errors.Is(err, domainsession.ErrCurrentPassword):
		httpapi.WriteError(c, 400, "CURRENT_PASSWORD_INCORRECT", "The current password is incorrect")
	case errors.Is(err, domainsession.ErrCredentialsChanged):
		httpapi.WriteError(c, 409, "CREDENTIALS_CHANGED", "Credentials have changed; sign in again")
	case errors.Is(err, domainaccount.ErrUnauthorized):
		c.Header("WWW-Authenticate", "Bearer")
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(err, applicationaccount.ErrPasswordBusy):
		c.Header("Retry-After", "1")
		httpapi.WriteError(c, 503, "AUTH_BUSY", "Please retry later")
	case errors.Is(err, context.Canceled):
	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, 504, "REQUEST_TIMEOUT", "Request timed out")
	default:
		httpapi.InternalError(c)
	}
}

func readLogin(c *gin.Context) (applicationsession.LoginInput, bool) {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Expected application/json")
		return applicationsession.LoginInput{}, false
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
		return applicationsession.LoginInput{}, false
	}

	var input LoginRequest
	if !utf8.Valid(raw) || json.Unmarshal(raw, &input) != nil {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		return applicationsession.LoginInput{}, false
	}

	return input.input(), true
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

	c.JSON(200, loginResponseFromResult(result))
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
			h.rejectSession(c, domainaccount.ErrUnauthorized)
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
		c.Request = c.Request.WithContext(interfaceshttpmiddleware.WithPrincipal(ctx, p.Principal))
		c.Next()
		return
	}
	if len(headers) != 1 {
		h.rejectSession(c, domainaccount.ErrUnauthorized)
		return
	}

	fields := strings.Fields(headers[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		h.rejectSession(c, domainaccount.ErrUnauthorized)
		return
	}

	p, err := h.service.Authenticate(c.Request.Context(), fields[1])
	if err != nil {
		h.rejectSession(c, err)
		return
	}

	c.Request = c.Request.WithContext(interfaceshttpmiddleware.WithPrincipal(c.Request.Context(), p))
	c.Next()
}

func (h *Handler) Me(c *gin.Context) {
	p, ok := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if !ok {
		writeError(c, domainaccount.ErrUnauthorized)
		return
	}

	result, err := h.service.Me(c.Request.Context(), p)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(200, userResponseFromResult(result))
}

func (h *Handler) Logout(c *gin.Context) {
	p, ok := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if !ok {
		writeError(c, domainaccount.ErrUnauthorized)
		return
	}
	if err := h.service.Logout(c.Request.Context(), p); err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (v LoginRequest) input() applicationsession.LoginInput {
	return applicationsession.LoginInput{
		Email:    v.Email,
		Password: v.Password,
	}
}

func (v PasswordChangeRequest) input() applicationsession.PasswordChangeInput {
	return applicationsession.PasswordChangeInput{
		CurrentPassword: v.CurrentPassword,
		NewPassword:     v.NewPassword,
	}
}

func userResponseFromResult(v applicationsession.Profile) UserResponse {
	return UserResponse{
		ID:        v.ID,
		Email:     v.Email,
		CreatedAt: v.CreatedAt,
	}
}

func loginResponseFromResult(v applicationsession.LoginResult) LoginResponse {
	return LoginResponse{
		AccessToken: v.AccessToken,
		TokenType:   v.TokenType,
		ExpiresAt:   v.ExpiresAt,
	}
}

func browserResponseFromResult(v applicationsession.BrowserResult) BrowserResponse {
	return BrowserResponse{
		User:      userResponseFromResult(v.User),
		ExpiresAt: v.ExpiresAt,
		CSRFToken: v.CSRFToken,
	}
}

func sessionPageFromResult(v applicationsession.SessionPageResult) SessionPage {
	return SessionPage{
		Items:      sessionInfoResponseFromResultList(v.Items),
		NextCursor: v.NextCursor,
	}
}

func sessionInfoResponseFromResult(v applicationsession.SessionInfoResult) SessionInfoResponse {
	return SessionInfoResponse{
		ID:             v.ID,
		CreatedAt:      v.CreatedAt,
		ExpiresAt:      v.ExpiresAt,
		CredentialKind: v.CredentialKind,
		Current:        v.Current,
	}
}

func sessionInfoResponseFromResultList(v []applicationsession.SessionInfoResult) []SessionInfoResponse {
	if v == nil {
		return nil
	}

	out := make([]SessionInfoResponse, len(v))
	for i := range v {
		out[i] = sessionInfoResponseFromResult(v[i])
	}

	return out
}
