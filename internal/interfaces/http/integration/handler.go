package interfaceshttpintegration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/gin-gonic/gin"
	applicationintegration "github.com/pluto-han/taskflow/internal/application/integration"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainintegration "github.com/pluto-han/taskflow/internal/domain/integration"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
)

func writeError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, domainaccount.ErrUnauthorized):
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(e, domainintegration.ErrLimited):
		c.Header("Retry-After", "60")
		httpapi.WriteError(c, 429, "DISCORD_CODE_RATE_LIMITED", "Please retry later")
	case errors.Is(e, domainintegration.ErrConflict):
		httpapi.WriteError(c, 409, "DISCORD_BINDING_CONFLICT", "Binding changed; reload before unbinding")
	case errors.Is(e, domainintegration.ErrInvalid):
		httpapi.WriteError(c, 400, "INVALID_DISCORD_BINDING", "Invalid binding request")
	default:
		httpapi.InternalError(c)
	}
}

type Handler struct {
	service *applicationintegration.Service
}

func New(service *applicationintegration.Service) *Handler { return &Handler{service: service} }

func (h *Handler) Status(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}

	out, e := h.service.Status(c.Request.Context(), owner)
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(200, bindingResponseFromResult(out))
}

func (h *Handler) Issue(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}

	if !h.service.Enabled() {
		httpapi.WriteError(c, 503, "DISCORD_DISABLED", "Discord integration is not configured")
		return
	}

	// No input fields; a binding code is always issued for the current principal.
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
	if e != nil || len(raw) > 4096 {
		writeError(c, domainintegration.ErrInvalid)
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var empty struct{}
		if decoder.Decode(&empty) != nil || decoder.Decode(new(any)) != io.EOF {
			writeError(c, domainintegration.ErrInvalid)
			return
		}
	}
	out, e := h.service.Issue(c.Request.Context(), owner)
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(201, codeResponseFromResult(out))
}

func (h *Handler) Unbind(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}

	var in UnbindRequest
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
		writeError(c, domainintegration.ErrInvalid)
		return
	}
	if e := h.service.Unbind(c.Request.Context(), owner, in.Version); e != nil {
		writeError(c, e)
		return
	}

	c.Status(204)
}

func (h *Handler) owner(c *gin.Context) (domainaccount.UserID, bool) {
	p, ok := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if !ok {
		writeError(c, domainaccount.ErrUnauthorized)
		return "", false
	}
	if h.service == nil {
		httpapi.WriteError(c, 503, "DISCORD_DISABLED", "Discord integration is not configured")
		return "", false
	}

	return p.UserID, true
}

func bindingResponseFromResult(v domainintegration.Binding) BindingResponse {
	return BindingResponse{
		Enabled:     v.Enabled,
		Version:     v.Version,
		Bound:       v.Bound,
		GuildID:     v.GuildID,
		ChannelID:   v.ChannelID,
		ChannelName: v.ChannelName,
		BoundAt:     v.BoundAt,
	}
}

func codeResponseFromResult(v domainintegration.Code) CodeResponse {
	return CodeResponse{
		Code:      v.Code,
		ExpiresAt: v.ExpiresAt,
	}
}
