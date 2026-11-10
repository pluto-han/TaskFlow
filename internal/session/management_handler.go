package session

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

func (h *Handler) ListSessions(c *gin.Context) {
	p, _ := CurrentPrincipal(c.Request.Context())
	page, err := h.service.ListSessions(c.Request.Context(), p, c.Request.URL.RawQuery)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) RevokeSession(c *gin.Context) {
	p, _ := CurrentPrincipal(c.Request.Context())
	if err := h.service.RevokeSession(c.Request.Context(), p, c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	if _, browser := c.Request.Context().Value(browserPrincipalKey{}).(BrowserPrincipal); browser && strings.EqualFold(p.SessionID, c.Param("id")) {
		h.clearCookie(c)
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ChangePassword(c *gin.Context) {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Expected application/json")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		httpapi.WriteError(c, 413, "REQUEST_TOO_LARGE", "Request body too large or unreadable")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input PasswordChangeRequest
	if !utf8.Valid(raw) || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid password change body")
		return
	}
	p, _ := CurrentPrincipal(c.Request.Context())
	if err := h.service.ChangePassword(c.Request.Context(), p, input); err != nil {
		writeError(c, err)
		return
	}
	if _, browser := c.Request.Context().Value(browserPrincipalKey{}).(BrowserPrincipal); browser {
		h.clearCookie(c)
	}
	c.Status(http.StatusNoContent)
}
