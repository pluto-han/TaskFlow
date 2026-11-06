package notification

import (
	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/session"
)

func Mount(router *gin.Engine, s *Service, auth gin.HandlerFunc) {
	router.GET("/integrations/discord/notifications", auth, func(c *gin.Context) {
		principal, ok := session.CurrentPrincipal(c.Request.Context())
		if !ok {
			httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
			return
		}
		if s == nil {
			httpapi.WriteError(c, 503, "DISCORD_DISABLED", "Discord integration is not configured")
			return
		}
		events, e := s.List(c.Request.Context(), string(principal.UserID))
		if e != nil {
			httpapi.InternalError(c)
			return
		}
		c.JSON(200, events)
	})
}
