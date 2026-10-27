// Package server owns the route registration shared by production and contracts.
package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/project"
	"github.com/pluto-han/taskflow/internal/session"
	"github.com/pluto-han/taskflow/internal/task"
	"github.com/pluto-han/taskflow/internal/user"
)

type Dependencies struct {
	Users    *user.Service
	Sessions *session.Service
	Tasks    *task.Service
	Projects *project.Service
	Ping     func(context.Context) error
	Logger   *slog.Logger
}

func NewRouter(d Dependencies) *gin.Engine {
	router := gin.New()
	// Avoid redirects outside the middleware chain: every response gets an ID.
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	router.Use(httpapi.Observe(logger))
	router.HandleMethodNotAllowed = true
	router.NoRoute(func(c *gin.Context) { httpapi.WriteError(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route not found") })
	router.NoMethod(func(c *gin.Context) {
		httpapi.WriteError(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	})
	httpapi.MountHealth(router, d.Ping)
	session.Mount(router, d.Sessions)
	router.POST("/auth/register", user.NewHandler(d.Users).Register)
	authentication := session.NewHandler(d.Sessions).RequireSession
	task.Mount(router, d.Tasks, authentication)
	project.Mount(router, d.Projects, authentication)
	return router
}
