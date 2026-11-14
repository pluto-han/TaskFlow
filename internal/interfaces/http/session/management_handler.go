package interfaceshttpsession

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
)

func (h *Handler) ListSessions(c *gin.Context) {
	p, _ := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	input, err := parseSessionListQuery(c.Request.URL.RawQuery)
	if err != nil {
		writeError(c, err)
		return
	}

	page, err := h.service.ListSessions(c.Request.Context(), p, input)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, sessionPageFromResult(page))
}

func (h *Handler) RevokeSession(c *gin.Context) {
	p, _ := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if err := h.service.RevokeSession(c.Request.Context(), p, c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	if _, browser := c.Request.Context().Value(browserPrincipalKey{}).(domainsession.BrowserPrincipal); browser && strings.EqualFold(p.SessionID, c.Param("id")) {
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

	p, _ := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if err := h.service.ChangePassword(c.Request.Context(), p, input.input()); err != nil {
		writeError(c, err)
		return
	}
	if _, browser := c.Request.Context().Value(browserPrincipalKey{}).(domainsession.BrowserPrincipal); browser {
		h.clearCookie(c)
	}
	c.Status(http.StatusNoContent)
}

func parseSessionListQuery(raw string) (applicationsession.ListInput, error) {
	input := applicationsession.ListInput{Limit: 20}
	params, err := url.ParseQuery(raw)
	if err != nil {
		return input, domainsession.ErrInvalidSessionQuery
	}

	for name, values := range params {
		if len(values) != 1 || values[0] == "" || (name != "limit" && name != "cursor") {
			return input, domainsession.ErrInvalidSessionQuery
		}
	}
	if value := params.Get("limit"); value != "" {
		input.Limit, err = strconv.Atoi(value)
		if err != nil || input.Limit < 1 || input.Limit > 100 {
			return input, domainsession.ErrInvalidSessionQuery
		}
	}
	input.Cursor = params.Get("cursor")

	return input, nil
}
