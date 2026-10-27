package task

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/session"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func writeTaskError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, session.ErrUnauthorized):
		c.Header("WWW-Authenticate", "Bearer")
		httpapi.WriteError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(err, ErrInvalidList):
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_QUERY", "Invalid task list parameters")
	case errors.Is(err, ErrTaskNotFound):
		httpapi.WriteError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")

	case errors.Is(err, ErrInvalidTask), errors.Is(err, ErrInvalidUpdate):
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_TASK", "Invalid task data")

	case errors.Is(err, context.Canceled):
		return

	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "Request timed out")

	case errors.Is(err, ErrTaskConflict):
		httpapi.WriteError(c, http.StatusConflict, "TASK_CONFLICT", "Task was modified by another request")

	default:
		httpapi.InternalError(c)
	}
}

func (h *Handler) GetByID(c *gin.Context) {
	ownerID, ok := authenticatedOwner(c)
	if !ok {
		return
	}
	id := c.Param("id")

	result, err := h.service.GetByID(c.Request.Context(), ownerID, id)

	if err != nil {
		writeTaskError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *Handler) List(c *gin.Context) {
	ownerID, ok := authenticatedOwner(c)
	if !ok {
		return
	}
	options, err := ParseListOptions(c.Request.URL.RawQuery)
	if err != nil {
		writeTaskError(c, err)
		return
	}
	page, err := h.service.List(c.Request.Context(), ownerID, options)

	if err != nil {
		writeTaskError(c, err)
		return
	}

	if page.NextCursor != "" {
		c.Header("X-Next-Cursor", page.NextCursor)
	}
	c.JSON(http.StatusOK, page.Items)
}

func (h *Handler) Create(c *gin.Context) {
	ownerID, ok := authenticatedOwner(c)
	if !ok {
		return
	}
	var input CreateTaskRequest

	if err := decodeTaskRequest(c, &input); err != nil {
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	createdTask, err := h.service.Create(c.Request.Context(), ownerID, input)

	if err != nil {
		writeTaskError(c, err)
		return
	}

	c.Header("Location", "/tasks/"+createdTask.ID)
	c.JSON(http.StatusCreated, createdTask)
}

func (h *Handler) Update(c *gin.Context) {
	ownerID, ok := authenticatedOwner(c)
	if !ok {
		return
	}
	id := c.Param("id")

	var input UpdateTaskRequest

	if err := decodeTaskRequest(c, &input); err != nil {
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	updated, err := h.service.Update(
		c.Request.Context(), ownerID,
		id,
		input,
	)

	if err != nil {
		writeTaskError(c, err)
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (h *Handler) Delete(c *gin.Context) {
	ownerID, ok := authenticatedOwner(c)
	if !ok {
		return
	}
	id := c.Param("id")

	if err := h.service.Delete(c.Request.Context(), ownerID, id); err != nil {
		writeTaskError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func authenticatedOwner(c *gin.Context) (session.UserID, bool) {
	p, ok := session.CurrentPrincipal(c.Request.Context())
	if !ok || p.UserID == "" {
		writeTaskError(c, session.ErrUnauthorized)
		return "", false
	}
	return p.UserID, true
}

// Ownership is server-assigned. Reject unknown fields (including owner_id/user_id)
// instead of silently accepting attempts to supply or change an identity.
func decodeTaskRequest(c *gin.Context, input any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return binding.Validator.ValidateStruct(input)
}

// Mount is the sole production task route registration path.
func Mount(router *gin.Engine, service *Service, authentication gin.HandlerFunc) {
	h := NewHandler(service)
	group := router.Group("/tasks", authentication)
	group.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() })
	group.GET("", h.List)
	group.GET("/:id", h.GetByID)
	group.POST("", h.Create)
	group.PATCH("/:id", h.Update)
	group.DELETE("/:id", h.Delete)
}
