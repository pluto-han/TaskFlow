package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/session"
)

func writeError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, session.ErrUnauthorized):
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(e, ErrLimited):
		c.Header("Retry-After", "60")
		httpapi.WriteError(c, 429, "DISCORD_CODE_RATE_LIMITED", "Please retry later")
	case errors.Is(e, ErrConflict):
		httpapi.WriteError(c, 409, "DISCORD_BINDING_CONFLICT", "Binding changed; reload before unbinding")
	case errors.Is(e, ErrInvalid):
		httpapi.WriteError(c, 400, "INVALID_DISCORD_BINDING", "Invalid binding request")
	default:
		httpapi.InternalError(c)
	}
}
func Mount(router *gin.Engine, s *Service, auth gin.HandlerFunc) {
	g := router.Group("/integrations/discord", auth)
	wrap := func(action func(*gin.Context, session.UserID)) gin.HandlerFunc {
		return func(c *gin.Context) {
			p, ok := session.CurrentPrincipal(c.Request.Context())
			if !ok {
				writeError(c, session.ErrUnauthorized)
				return
			}
			if s == nil {
				httpapi.WriteError(c, 503, "DISCORD_DISABLED", "Discord integration is not configured")
				return
			}
			action(c, p.UserID)
		}
	}
	g.GET("", wrap(func(c *gin.Context, owner session.UserID) {
		out, e := s.Status(c.Request.Context(), owner)
		if e != nil {
			writeError(c, e)
			return
		}
		c.JSON(200, out)
	}))
	g.POST("/code", wrap(func(c *gin.Context, owner session.UserID) {
		if !s.Enabled() {
			httpapi.WriteError(c, 503, "DISCORD_DISABLED", "Discord integration is not configured")
			return
		}
		// No input fields; a binding code is always issued for the current principal.
		raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
		if e != nil || len(raw) > 4096 {
			writeError(c, ErrInvalid)
			return
		}
		if len(bytes.TrimSpace(raw)) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			var empty struct{}
			if decoder.Decode(&empty) != nil || decoder.Decode(new(any)) != io.EOF {
				writeError(c, ErrInvalid)
				return
			}
		}
		out, e := s.Issue(c.Request.Context(), owner)
		if e != nil {
			writeError(c, e)
			return
		}
		c.JSON(201, out)
	}))
	g.DELETE("", wrap(func(c *gin.Context, owner session.UserID) {
		var in UnbindRequest
		d := json.NewDecoder(c.Request.Body)
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
			writeError(c, ErrInvalid)
			return
		}
		if e := s.Unbind(c.Request.Context(), owner, in.Version); e != nil {
			writeError(c, e)
			return
		}
		c.Status(204)
	}))
}
