package interfaceshttptask

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
)

type Handler struct {
	service *applicationtask.Service
}

func New(service *applicationtask.Service) *Handler {
	return &Handler{service: service}
}

func writeTaskError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domaintask.ErrPlanDayChanged):
		httpapi.WriteError(c, http.StatusConflict, "PLAN_DAY_CHANGED", "The plan date has changed; refresh today and submit again")
	case errors.Is(err, domainaccount.ErrUnauthorized):
		c.Header("WWW-Authenticate", "Bearer")
		httpapi.WriteError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(err, domainproject.ErrForbidden):
		httpapi.WriteError(c, 403, "FORBIDDEN", "Operation not permitted")
	case errors.Is(err, domainproject.ErrInvalid):
		httpapi.WriteError(c, 400, "INVALID_PROJECT", "Invalid project ID")
	case errors.Is(err, domainproject.ErrNotFound):
		httpapi.WriteError(c, 404, "PROJECT_NOT_FOUND", "Project not found")
	case errors.Is(err, domaintask.ErrInvalidList):
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_QUERY", "Invalid task list parameters")
	case errors.Is(err, domaintask.ErrTaskNotFound):
		httpapi.WriteError(c, http.StatusNotFound, "TASK_NOT_FOUND", "Task not found")

	case errors.Is(err, domaintask.ErrInvalidTask), errors.Is(err, domaintask.ErrInvalidUpdate):
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_TASK", "Invalid task data")

	case errors.Is(err, context.Canceled):
		return

	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "Request timed out")

	case errors.Is(err, domaintask.ErrTaskConflict):
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

	c.JSON(http.StatusOK, taskResponseFromResultPtr(result))
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
	c.JSON(http.StatusOK, taskResponseFromResultList(page.Items))
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

	createdTask, err := h.service.Create(c.Request.Context(), ownerID, input.input())

	if err != nil {
		writeTaskError(c, err)
		return
	}

	c.Header("Location", "/tasks/"+createdTask.ID)
	c.JSON(http.StatusCreated, taskResponseFromResultPtr(createdTask))
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
		input.input(),
	)

	if err != nil {
		writeTaskError(c, err)
		return
	}

	c.JSON(http.StatusOK, taskResponseFromResultPtr(updated))
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

func authenticatedOwner(c *gin.Context) (domainaccount.UserID, bool) {
	p, ok := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if !ok || p.UserID == "" {
		writeTaskError(c, domainaccount.ErrUnauthorized)
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

func (h *Handler) ListToday(c *gin.Context) {
	owner, ok := authenticatedOwner(c)
	if !ok {
		return
	}

	options, err := ParseListOptions(c.Request.URL.RawQuery)
	if err != nil {
		writeTaskError(c, err)
		return
	}

	page, err := h.service.ListToday(c.Request.Context(), owner, options)
	if err != nil {
		writeTaskError(c, err)
		return
	}
	if page.NextCursor != "" {
		c.Header("X-Next-Cursor", page.NextCursor)
	}
	c.JSON(http.StatusOK, todayPageResponseFromResult(page))
}

func (h *Handler) CreateToday(c *gin.Context) {
	owner, ok := authenticatedOwner(c)
	if !ok {
		return
	}

	var input CreateTodayTaskRequest
	if err := decodeTaskRequest(c, &input); err != nil {
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	item, err := h.service.CreateToday(c.Request.Context(), owner, input.input())
	if err != nil {
		writeTaskError(c, err)
		return
	}

	c.Header("Location", "/tasks/"+item.ID)
	c.JSON(http.StatusCreated, taskResponseFromResultPtr(item))
}

// ParseListOptions rejects empty/duplicate known parameters. Unknown parameters
// cannot select a user; identity is always taken from authenticated context.
func ParseListOptions(rawQuery string) (applicationtask.ListOptions, error) {
	values, err := url.ParseQuery(rawQuery)
	options := applicationtask.ListOptions{Limit: applicationtask.DefaultLimit}
	if err != nil {
		return options, domaintask.ErrInvalidList
	}

	for _, key := range []string{
		"limit",
		"cursor",
		"completed",
		"priority",
		"has_due_date",
		"due_from",
		"due_before",
		"project_id",
		"plan_date",
	} {
		v, exists := values[key]
		if !exists {
			continue
		}
		if len(v) != 1 || v[0] == "" {
			return options, domaintask.ErrInvalidList
		}

		switch key {
		case "plan_date":
			value := v[0]
			options.PlanDate = &value
		case "project_id":
			value := v[0]
			options.ProjectID = &value
		case "limit":
			options.Limit, err = strconv.Atoi(v[0])
			if err != nil || options.Limit < 1 || options.Limit > applicationtask.MaxLimit {
				return options, domaintask.ErrInvalidList
			}
		case "cursor":
			if len(v[0]) > 2048 {
				return options, domaintask.ErrInvalidList
			}
			options.Cursor = v[0]
		case "priority":
			number, e := strconv.Atoi(v[0])
			if e != nil {
				return options, domaintask.ErrInvalidList
			}
			options.Priority = &number
		case "completed", "has_due_date":
			if v[0] != "true" && v[0] != "false" {
				return options, domaintask.ErrInvalidList
			}
			value := v[0] == "true"
			if key == "completed" {
				options.Completed = &value
			} else {
				options.HasDueDate = &value
			}
		case "due_from", "due_before":
			value, e := time.Parse(time.RFC3339Nano, v[0])
			if e != nil {
				return options, domaintask.ErrInvalidList
			}
			if key == "due_from" {
				options.DueFrom = &value
			} else {
				options.DueBefore = &value
			}
		}
	}
	options.ListFilter, err = domaintask.NormalizeFilter(options.ListFilter)

	return options, err
}

func (v CreateTaskRequest) input() applicationtask.CreateInput {
	return applicationtask.CreateInput{
		ProjectID:   v.ProjectID,
		Title:       v.Title,
		Description: v.Description,
		Priority:    v.Priority,
		DueDate:     v.DueDate,
	}
}

func (v UpdateTaskRequest) input() applicationtask.UpdateInput {
	return applicationtask.UpdateInput{
		ProjectID:   v.ProjectID.input(),
		DueDate:     v.DueDate.input(),
		Title:       v.Title,
		Description: v.Description,
		Priority:    v.Priority,
		Completed:   v.Completed,
		Version:     v.Version,
	}
}

func (v CreateTodayTaskRequest) input() applicationtask.CreateTodayInput {
	return applicationtask.CreateTodayInput{
		PlanDate:    v.PlanDate,
		Title:       v.Title,
		Description: v.Description,
		Priority:    v.Priority,
		DueDate:     v.DueDate,
	}
}

func (v OptionalTime) input() applicationtask.OptionalTime {
	return applicationtask.OptionalTime{
		Present: v.Present,
		Value:   v.Value,
	}
}

func (v OptionalString) input() applicationtask.OptionalString {
	return applicationtask.OptionalString{
		Present: v.Present,
		Value:   v.Value,
	}
}

func todayPageResponseFromResult(v applicationtask.TodayResult) TodayPageResponse {
	return TodayPageResponse{
		Items:      taskResponseFromResultList(v.Items),
		NextCursor: v.NextCursor,
		PlanDate:   v.PlanDate,
		Timezone:   v.Timezone,
		ServerTime: v.ServerTime,
		NextDayAt:  v.NextDayAt,
	}
}

func taskResponseFromResult(v applicationtask.TaskResult) TaskResponse {
	return TaskResponse{
		RecurrenceID: v.RecurrenceID,
		PlanDate:     v.PlanDate,
		CanMove:      v.CanMove,
		ProjectID:    v.ProjectID,
		OwnerID:      v.OwnerID,
		ID:           v.ID,
		Title:        v.Title,
		Description:  v.Description,
		Completed:    v.Completed,
		Priority:     v.Priority,
		CreatedAt:    v.CreatedAt,
		DueDate:      v.DueDate,
		Version:      v.Version,
	}
}

func taskResponseFromResultPtr(v *applicationtask.TaskResult) *TaskResponse {
	if v == nil {
		return nil
	}

	out := taskResponseFromResult(*v)

	return &out
}

func taskResponseFromResultList(v []applicationtask.TaskResult) []TaskResponse {
	if v == nil {
		return nil
	}

	out := make([]TaskResponse, len(v))
	for i := range v {
		out[i] = taskResponseFromResult(v[i])
	}

	return out
}
