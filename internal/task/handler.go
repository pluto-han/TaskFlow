package task

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func writeTaskError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		httpapi.WriteError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")

	case errors.Is(err, ErrInvalidTask), errors.Is(err, ErrInvalidUpdate):
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_TASK", "Invalid task data")

	case errors.Is(err, context.Canceled):
		return

	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "Request timed out")

	default:
		httpapi.InternalError(c)
	}
}

func (h *Handler) GetByID(c *gin.Context) {
	id := c.Param("id")

	result, err := h.service.GetByID(c.Request.Context(), id)

	if err != nil {
		writeTaskError(c, err)
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *Handler) List(c *gin.Context) {
	tasks, err := h.service.List(c.Request.Context())

	if err != nil {
		writeTaskError(c, err)
		return
	}

	c.JSON(http.StatusOK, tasks)
}

func (h *Handler) Create(c *gin.Context) {
	var input CreateTaskRequest

	if err := c.ShouldBindJSON(&input); err != nil {
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	createdTask, err := h.service.Create(c.Request.Context(), input)

	if err != nil {
		writeTaskError(c, err)
		return
	}

	c.Header("Location", "/tasks/"+createdTask.ID)
	c.JSON(http.StatusCreated, createdTask)
}

func (h *Handler) Update(c *gin.Context) {
	id := c.Param("id")

	var input UpdateTaskRequest

	if err := c.ShouldBindJSON(&input); err != nil {
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	updated, err := h.service.Update(
		c.Request.Context(),
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
	id := c.Param("id")

	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		writeTaskError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
