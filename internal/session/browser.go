package session

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

const secureCookieName = "__Host-taskflow_session"
const developmentCookieName = "taskflow_session_dev"

type browserPrincipalKey struct{}

func (h *Handler) cookieName() string {
	if h.secureCookie {
		return secureCookieName
	}
	return developmentCookieName
}

// Count raw names too: malformed or empty recognized cookies must not permit a
// fallback to Bearer. Request.Cookies alone silently ignores malformed values.
func (h *Handler) sessionCookie(r *http.Request) (value string, present, valid bool) {
	count := 0
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if name == secureCookieName || name == developmentCookieName {
				count++
			}
		}
	}
	if count == 0 {
		return "", false, false
	}
	cookies := r.CookiesNamed(h.cookieName())
	if count != 1 || len(cookies) != 1 {
		return "", true, false
	}
	return cookies[0].Value, true, true
}

func ambiguousCredentials(c *gin.Context) {
	httpapi.WriteError(c, 400, "AMBIGUOUS_CREDENTIALS", "Use exactly one authentication mechanism")
	c.Abort()
}

func (h *Handler) rejectSession(c *gin.Context, err error) {
	if errors.Is(err, ErrUnauthorized) && c.FullPath() == "/auth/browser/session" {
		h.clearCookie(c)
	}
	writeError(c, err)
	c.Abort()
}

func (h *Handler) bearerOnly(c *gin.Context) bool {
	_, present, _ := h.sessionCookie(c.Request)
	if !present {
		return true
	}
	if len(c.Request.Header.Values("Authorization")) > 0 {
		ambiguousCredentials(c)
	} else {
		h.rejectSession(c, ErrUnauthorized)
	}
	return false
}

func (h *Handler) requireBearer(c *gin.Context) {
	if h.bearerOnly(c) {
		h.RequireSession(c)
	}
}

func (h *Handler) requireBrowser(c *gin.Context) {
	_, present, valid := h.sessionCookie(c.Request)
	if len(c.Request.Header.Values("Authorization")) > 0 {
		if present {
			ambiguousCredentials(c)
		} else {
			h.rejectSession(c, ErrUnauthorized)
		}
		return
	}
	if !present || !valid {
		h.rejectSession(c, ErrUnauthorized)
		return
	}
	h.RequireSession(c)
}

func isMutation(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func (h *Handler) browserOriginAllowed(c *gin.Context, required bool) bool {
	origins := c.Request.Header.Values("Origin")
	sites := c.Request.Header.Values("Sec-Fetch-Site")
	if h.browserOrigin == "" || len(origins) > 1 ||
		(required && len(origins) != 1) ||
		(len(origins) == 1 && origins[0] != h.browserOrigin) ||
		len(sites) > 1 || (len(sites) == 1 && sites[0] == "cross-site") {
		httpapi.WriteError(c, 403, "ORIGIN_NOT_ALLOWED", "Origin is not permitted")
		c.Abort()
		return false
	}
	return true
}

func validCSRF(c *gin.Context, secret [32]byte) bool {
	headers := c.Request.Header.Values("X-CSRF-Token")
	if len(headers) == 1 && len(headers[0]) == 43 {
		raw, err := base64.RawURLEncoding.Strict().DecodeString(headers[0])
		if err == nil && len(raw) == 32 && subtle.ConstantTimeCompare(raw, secret[:]) == 1 {
			return true
		}
	}
	httpapi.WriteError(c, 403, "CSRF_INVALID", "Refresh your session before retrying this request")
	c.Abort()
	return false
}

func (h *Handler) setCookie(c *gin.Context, token string, expiry time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: h.cookieName(), Value: token, Path: "/",
		Expires: expiry.UTC().Truncate(time.Second), MaxAge: int(time.Until(expiry) / time.Second),
		HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) clearCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: h.cookieName(), Value: "", Path: "/", Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
		HttpOnly: true, Secure: h.secureCookie, SameSite: http.SameSiteLaxMode,
	})
}

func (h *Handler) BrowserLogin(c *gin.Context) {
	_, present, valid := h.sessionCookie(c.Request)
	if len(c.Request.Header.Values("Authorization")) > 0 {
		ambiguousCredentials(c)
		return
	}
	if present && !valid {
		h.rejectSession(c, ErrUnauthorized)
		return
	}
	if !h.browserOriginAllowed(c, true) {
		return
	}
	input, ok := readLogin(c)
	if !ok {
		return
	}
	result, token, err := h.service.LoginBrowser(c.Request.Context(), input)
	if err != nil {
		writeError(c, err)
		return
	}
	h.setCookie(c, token, result.ExpiresAt)
	c.JSON(http.StatusOK, result)
}

func (h *Handler) BrowserSession(c *gin.Context) {
	p := c.Request.Context().Value(browserPrincipalKey{}).(BrowserPrincipal)
	u, err := h.service.Me(c.Request.Context(), p.Principal)
	if err != nil {
		h.rejectSession(c, err)
		return
	}
	c.JSON(http.StatusOK, BrowserResponse{User: u, ExpiresAt: p.ExpiresAt,
		CSRFToken: base64.RawURLEncoding.EncodeToString(p.CSRFSecret[:])})
}

func (h *Handler) BrowserLogout(c *gin.Context) {
	p, _ := CurrentPrincipal(c.Request.Context())
	if err := h.service.Logout(c.Request.Context(), p); err != nil {
		writeError(c, err)
		return
	}
	h.clearCookie(c)
	c.Status(http.StatusNoContent)
}
