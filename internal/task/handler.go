package task

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetByID(c *gin.Context) {
	id := c.Param("id")

	task, err := h.service.GetByID(c.Request.Context(), id)

	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "task not found",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, task)
}

func (h *Handler) List(c *gin.Context) {
	tasks, err := h.service.List(c.Request.Context())

	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "task not found",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.JSON(http.StatusOK, tasks)
}

func (h *Handler) Create(c *gin.Context) {
	var input CreateTaskRequest

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	createdTask, err := h.service.Create(c.Request.Context(), input)

	if err != nil {
		if errors.Is(err, ErrInvalidTask) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid task",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.Header("Location", "/tasks/"+createdTask.ID)
	c.JSON(http.StatusCreated, createdTask)
}

func (h *Handler) Update(c *gin.Context) {
	id := c.Param("id")

	var input UpdateTaskRequest

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	updated, err := h.service.Update(
		c.Request.Context(),
		id,
		input,
	)

	if err != nil {
		switch {
		case errors.Is(err, ErrTaskNotFound):
			c.JSON(http.StatusNotFound, gin.H{
				"error": "task not found",
			})

		case errors.Is(err, ErrInvalidUpdate):
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "invalid task update",
			})

		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "internal server error",
			})
		}
		return
	}

	c.JSON(http.StatusOK, updated)
}

func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")

	err := h.service.Delete(
		c.Request.Context(),
		id,
	)

	if err != nil {
		if errors.Is(err, ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "task not found",
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
		return
	}

	c.Status(http.StatusNoContent)
}
