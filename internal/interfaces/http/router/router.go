// Package interfaceshttprouter owns route registration shared by production and contracts.
package interfaceshttprouter

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	applicationintegration "github.com/pluto-han/taskflow/internal/application/integration"
	applicationnotification "github.com/pluto-han/taskflow/internal/application/notification"
	applicationproject "github.com/pluto-han/taskflow/internal/application/project"
	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpaccount "github.com/pluto-han/taskflow/internal/interfaces/http/account"
	interfaceshttpdiscord "github.com/pluto-han/taskflow/internal/interfaces/http/discord"
	interfaceshttpintegration "github.com/pluto-han/taskflow/internal/interfaces/http/integration"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
	interfaceshttpnotification "github.com/pluto-han/taskflow/internal/interfaces/http/notification"
	interfaceshttpproject "github.com/pluto-han/taskflow/internal/interfaces/http/project"
	interfaceshttprecurrence "github.com/pluto-han/taskflow/internal/interfaces/http/recurrence"
	interfaceshttpsession "github.com/pluto-han/taskflow/internal/interfaces/http/session"
	interfaceshttptask "github.com/pluto-han/taskflow/internal/interfaces/http/task"
)

type Dependencies struct {
	Mail          *applicationaccount.CapabilityService
	Notifications *applicationnotification.Service
	Discord       *interfaceshttpdiscord.Handler
	Integrations  *applicationintegration.Service
	Recurrences   *applicationrecurrence.Service
	Metrics       interfaceshttpmiddleware.RequestObserver
	Security      interfaceshttpmiddleware.SecurityOptions
	Users         *applicationaccount.Service
	Sessions      *applicationsession.Service
	Tasks         *applicationtask.Service
	Projects      *applicationproject.Service
	Ping          func(context.Context) error
	Logger        *slog.Logger
}

func New(d Dependencies) *gin.Engine {
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	// Avoid redirects outside the middleware chain: every response gets an ID.
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	router.Use(interfaceshttpmiddleware.Observe(logger, d.Metrics), interfaceshttpmiddleware.Security(d.Security))
	router.HandleMethodNotAllowed = true
	router.NoRoute(func(c *gin.Context) { httpapi.WriteError(c, http.StatusNotFound, "ROUTE_NOT_FOUND", "Route not found") })
	router.NoMethod(func(c *gin.Context) {
		httpapi.WriteError(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
	})
	MountHealth(router, d.Ping)
	sessions := RegisterSessions(router, d.Sessions, d.Security)
	RegisterAccounts(router, d.Users)
	authentication := sessions.RequireSession
	RegisterMails(router, d.Mail, authentication)
	router.POST(interfaceshttpdiscord.Path, d.Discord.Handle)
	RegisterIntegrations(router, d.Integrations, authentication)
	RegisterNotifications(router, d.Notifications, authentication)
	RegisterTasks(router, d.Tasks, authentication)
	RegisterProjects(router, d.Projects, authentication)
	RegisterRecurrences(router, d.Recurrences, authentication)

	return router
}

func RegisterAccounts(router *gin.Engine, service *applicationaccount.Service) {
	router.POST("/auth/register", interfaceshttpaccount.New(service).Register)
}

func RegisterTasks(
	router *gin.Engine,
	service *applicationtask.Service,
	authentication gin.HandlerFunc,
) {
	h := interfaceshttptask.New(service)
	group := router.Group("/tasks", authentication)
	group.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() })
	group.GET("", h.List)
	group.GET("/today", h.ListToday)
	group.POST("/today", h.CreateToday)
	group.GET("/:id", h.GetByID)
	group.POST("", h.Create)
	group.PATCH("/:id", h.Update)
	group.DELETE("/:id", h.Delete)
}

func RegisterProjects(
	router *gin.Engine,
	service *applicationproject.Service,
	authentication gin.HandlerFunc,
) {
	h := interfaceshttpproject.New(service)
	group := router.Group("/projects", authentication)
	group.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() })
	group.POST("", h.Create)
	group.GET("", h.List)
	group.GET("/:id", h.Get)
	group.PATCH("/:id", h.Update)
	group.DELETE("/:id", h.Delete)
	group.GET("/:id/members", h.Members)
	group.DELETE("/:id/members/:userID", h.RemoveMember)
	group.POST("/:id/leave", h.Leave)
	group.POST("/:id/ownership", h.Transfer)
	group.GET("/:id/invitations", h.Invitations)
	group.POST("/:id/invitations", h.Invite)
	group.DELETE("/:id/invitations/:invitationID", h.RevokeInvitation)
	router.POST("/invitations/accept", authentication, func(c *gin.Context) { c.Header("Cache-Control", "no-store"); h.AcceptInvitation(c) })
}

func RegisterSessions(
	router *gin.Engine,
	service *applicationsession.Service,
	options ...interfaceshttpmiddleware.SecurityOptions,
) *interfaceshttpsession.Handler {
	h := interfaceshttpsession.New(service, options...)
	group := router.Group("/auth")
	group.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Header("Pragma", "no-cache"); c.Next() })
	limiter := interfaceshttpsession.NewLoginLimiter()
	group.POST("/login", limiter.Middleware, h.Login)
	group.GET("/me", h.RequireBearer, h.Me)
	group.POST("/logout", h.RequireBearer, h.Logout)
	group.POST("/browser/login", limiter.Middleware, h.BrowserLogin)
	group.GET("/browser/session", h.RequireBrowser, h.BrowserSession)
	group.POST("/browser/logout", h.RequireBrowser, h.BrowserLogout)
	group.GET("/sessions", h.RequireSession, h.ListSessions)
	group.DELETE("/sessions/:id", h.RequireSession, h.RevokeSession)
	group.POST("/password/change", h.RequireSession, h.ChangePassword)

	return h
}

func RegisterRecurrences(
	router *gin.Engine,
	service *applicationrecurrence.Service,
	auth gin.HandlerFunc,
) {
	h := interfaceshttprecurrence.New(service)
	g := router.Group("/recurrences", auth)
	g.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() })
	g.GET("", h.List)
	g.POST("/preview", h.Preview)
	g.POST("", h.Create)
	g.GET("/:id", h.Get)
	g.PATCH("/:id", h.Update)
	g.DELETE("/:id", h.Delete)
}

func RegisterIntegrations(
	router *gin.Engine,
	service *applicationintegration.Service,
	auth gin.HandlerFunc,
) {
	h := interfaceshttpintegration.New(service)
	g := router.Group("/integrations/discord", auth)
	g.GET("", h.Status)
	g.POST("/code", h.Issue)
	g.DELETE("", h.Unbind)
}

func RegisterNotifications(
	router *gin.Engine,
	service *applicationnotification.Service,
	auth gin.HandlerFunc,
) {
	h := interfaceshttpnotification.New(service)
	router.GET("/integrations/discord/notifications", auth, h.List)
}

func RegisterMails(
	router *gin.Engine,
	service *applicationaccount.CapabilityService,
	auth gin.HandlerFunc,
) {
	h := interfaceshttpaccount.NewCapabilities(service)
	router.POST("/auth/password/reset/request", h.RequireEnabled, h.RequestReset)
	router.POST("/auth/password/reset/confirm", h.RequireEnabled, h.ConfirmReset)
	router.POST("/auth/email/verification", h.RequireEnabled, auth, h.Request)
	router.POST("/auth/email/verification/confirm", h.RequireEnabled, h.Confirm)
	router.GET("/auth/email/verification", auth, h.Status)
}
