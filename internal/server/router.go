// Package server owns the route registration shared by production and contracts.
package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/discord"
	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/integration"
	"github.com/pluto-han/taskflow/internal/notification"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/recurrence"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/user"
)

type Dependencies struct {
	Notifications *notification.Service
	Discord       *discord.Handler
	Integrations  *integration.Service
	Recurrences   *recurrence.Service
	Metrics       httpapi.RequestObserver
	Security      httpapi.SecurityOptions
	Users         *user.Service
	Sessions      *session.Service
	Tasks         *task.Service
	Projects      *project.Service
	Ping          func(context.Context) error
	Logger        *slog.Logger
}

func NewRouter(d Dependencies) *gin.Engine {
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	// Avoid redirects outside the middleware chain: every response gets an ID.
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	router.Use(httpapi.Observe(logger, d.Metrics), httpapi.Security(d.Security))
	router.HandleMethodNotAllowed = true
	router.NoRoute(func(c *gin.Context) { httpapi.WriteError(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route not found") })
	router.NoMethod(func(c *gin.Context) {
		httpapi.WriteError(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	})
	httpapi.MountHealth(router, d.Ping)
	sessions := session.Mount(router, d.Sessions, d.Security)
	router.POST("/auth/register", user.NewHandler(d.Users).Register)
	authentication := sessions.RequireSession
	router.POST(discord.Path, d.Discord.Handle)
	integration.Mount(router, d.Integrations, authentication)
	notification.Mount(router, d.Notifications, authentication)
	task.Mount(router, d.Tasks, authentication)
	project.Mount(router, d.Projects, authentication)
	recurrence.Mount(router, d.Recurrences, authentication)
	return router
}
