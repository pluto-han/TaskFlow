package project

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/session"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, session.ErrUnauthorized):
		c.Header("WWW-Authenticate", "Bearer")
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(err, ErrForbidden):
		httpapi.WriteError(c, 403, "FORBIDDEN", "Operation not permitted")
	case errors.Is(err, ErrInvitationInvalid):
		httpapi.WriteError(c, 400, "INVITATION_INVALID", "Invitation is invalid or unavailable")
	case errors.Is(err, ErrInvalidInvitee):
		httpapi.WriteError(c, 400, "INVALID_INVITEE", "Recipient or ownership target is invalid")
	case errors.Is(err, ErrOwnerRequired):
		httpapi.WriteError(c, 409, "OWNER_REQUIRED", "Transfer ownership before leaving")
	case errors.Is(err, ErrInvalid):
		httpapi.WriteError(c, 400, "INVALID_PROJECT", "Invalid project data")
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(c, 404, "PROJECT_NOT_FOUND", "Project not found")
	case errors.Is(err, ErrNotEmpty):
		httpapi.WriteError(c, 409, "PROJECT_NOT_EMPTY", "Move or delete tasks before deleting the project")
	case errors.Is(err, context.Canceled):
	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, 504, "REQUEST_TIMEOUT", "Request timed out")
	default:
		httpapi.InternalError(c)
	}
}
func owner(c *gin.Context) (session.UserID, bool) {
	p, ok := session.CurrentPrincipal(c.Request.Context())
	if !ok || p.UserID == "" {
		writeError(c, session.ErrUnauthorized)
		return "", false
	}
	return p.UserID, true
}
func decode(c *gin.Context, input any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		return false
	}
	return true
}
func (h *Handler) Create(c *gin.Context) {
	uid, ok := owner(c)
	if !ok {
		return
	}
	var input CreateRequest
	if !decode(c, &input) {
		return
	}
	result, err := h.service.Create(c.Request.Context(), uid, input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Location", "/projects/"+result.ID)
	c.JSON(201, result)
}
func (h *Handler) Get(c *gin.Context) {
	uid, ok := owner(c)
	if !ok {
		return
	}
	result, err := h.service.GetByID(c.Request.Context(), uid, c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, result)
}
func (h *Handler) List(c *gin.Context) {
	uid, ok := owner(c)
	if !ok {
		return
	}
	result, err := h.service.List(c.Request.Context(), uid)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, result)
}
func (h *Handler) Update(c *gin.Context) {
	uid, ok := owner(c)
	if !ok {
		return
	}
	var input UpdateRequest
	if !decode(c, &input) {
		return
	}
	result, err := h.service.Update(c.Request.Context(), uid, c.Param("id"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, result)
}
func (h *Handler) Delete(c *gin.Context) {
	uid, ok := owner(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), uid, c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(204)
}
func Mount(router *gin.Engine, service *Service, authentication gin.HandlerFunc) {
	h := NewHandler(service)
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
