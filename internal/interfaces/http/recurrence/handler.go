package interfaceshttprecurrence

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	applicationrecurrence "github.com/pluto-han/taskflow/internal/application/recurrence"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainrecurrence "github.com/pluto-han/taskflow/internal/domain/recurrence"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
)

func writeError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, domainaccount.ErrUnauthorized):
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(e, domainrecurrence.ErrInvalid):
		httpapi.WriteError(c, 400, "INVALID_RECURRENCE", "Invalid recurrence data or query")
	case errors.Is(e, domainrecurrence.ErrNotFound):
		httpapi.WriteError(c, 404, "RECURRENCE_NOT_FOUND", "Recurrence not found")
	case errors.Is(e, domainrecurrence.ErrConflict):
		httpapi.WriteError(c, 409, "RECURRENCE_CONFLICT", "Recurrence changed; reload before editing")
	case errors.Is(e, context.Canceled):
	case errors.Is(e, context.DeadlineExceeded):
		httpapi.WriteError(c, 504, "REQUEST_TIMEOUT", "Request timed out")
	default:
		httpapi.InternalError(c)
	}
}

func decode(c *gin.Context, out any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		return false
	}
	if e := d.Decode(new(any)); e != io.EOF {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		return false
	}

	return true
}

type Handler struct {
	service *applicationrecurrence.Service
}

func New(service *applicationrecurrence.Service) *Handler { return &Handler{service: service} }

func (h *Handler) List(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}

	values, e := url.ParseQuery(c.Request.URL.RawQuery)
	if e != nil {
		writeError(c, domainrecurrence.ErrInvalid)
		return
	}

	limit := 20
	for k, v := range values {
		if (k != "cursor" && k != "limit") || len(v) != 1 || v[0] == "" {
			writeError(c, domainrecurrence.ErrInvalid)
			return
		}
	}
	if values.Has("limit") {
		limit, e = strconv.Atoi(values.Get("limit"))
		if e != nil {
			writeError(c, domainrecurrence.ErrInvalid)
			return
		}
	}
	page, e := h.service.List(c.Request.Context(), owner, values.Get("cursor"), limit)
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(200, pageResponseFromResult(page))
}

func (h *Handler) Preview(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}

	var in PreviewRequest
	if !decode(c, &in) {
		return
	}

	out, e := h.service.Preview(c.Request.Context(), owner, in.input())
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(200, previewResponseFromResult(out))
}

func (h *Handler) Create(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}

	var in CreateRequest
	if !decode(c, &in) {
		return
	}

	out, e := h.service.Create(c.Request.Context(), owner, in.input())
	if e != nil {
		writeError(c, e)
		return
	}

	c.Header("Location", "/recurrences/"+out.ID)
	c.JSON(201, ruleResponseFromResultPtr(out))
}

func (h *Handler) Get(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}

	out, e := h.service.Get(c.Request.Context(), owner, c.Param("id"))
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(200, ruleResponseFromResultPtr(out))
}

func (h *Handler) Update(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}

	var in UpdateRequest
	if !decode(c, &in) {
		return
	}

	out, e := h.service.Update(c.Request.Context(), owner, c.Param("id"), in.input())
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(200, ruleResponseFromResultPtr(out))
}

func (h *Handler) Delete(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}

	var in DeleteRequest
	if !decode(c, &in) {
		return
	}
	if e := h.service.Delete(c.Request.Context(), owner, c.Param("id"), in.Version); e != nil {
		writeError(c, e)
		return
	}

	c.Status(204)
}

func (h *Handler) owner(c *gin.Context) (domainaccount.UserID, bool) {
	p, ok := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if !ok || p.UserID == "" {
		writeError(c, domainaccount.ErrUnauthorized)
		return "", false
	}

	return p.UserID, true
}

func (v CreateRequest) input() applicationrecurrence.CreateInput {
	return applicationrecurrence.CreateInput{
		ReminderEnabled: v.ReminderEnabled,
		Title:           v.Title,
		Description:     v.Description,
		Priority:        v.Priority,
		AnchorDate:      v.AnchorDate,
		IntervalDays:    v.IntervalDays,
	}
}

func (v UpdateRequest) input() applicationrecurrence.UpdateInput {
	return applicationrecurrence.UpdateInput{
		ReminderEnabled: v.ReminderEnabled,
		Version:         v.Version,
		Title:           v.Title,
		Description:     v.Description,
		Priority:        v.Priority,
		AnchorDate:      v.AnchorDate,
		IntervalDays:    v.IntervalDays,
		Paused:          v.Paused,
	}
}

func (v PreviewRequest) input() applicationrecurrence.PreviewInput {
	return applicationrecurrence.PreviewInput{
		AnchorDate:   v.AnchorDate,
		IntervalDays: v.IntervalDays,
	}
}

func previewResponseFromResult(v applicationrecurrence.PreviewResult) PreviewResponse {
	return PreviewResponse{
		PlanDate: v.PlanDate,
		Timezone: v.Timezone,
		Dates:    v.Dates,
	}
}

func pageResponseFromResult(v applicationrecurrence.PageResult) PageResponse {
	return PageResponse{
		Items:            ruleResponseFromResultList(v.Items),
		NextCursor:       v.NextCursor,
		PlanDate:         v.PlanDate,
		Timezone:         v.Timezone,
		SchedulerEnabled: v.SchedulerEnabled,
	}
}

func ruleResponseFromResult(v applicationrecurrence.RuleResult) RuleResponse {
	return RuleResponse{
		ReminderEnabled: v.ReminderEnabled,
		ID:              v.ID,
		OwnerID:         v.OwnerID,
		Title:           v.Title,
		Description:     v.Description,
		Priority:        v.Priority,
		AnchorDate:      v.AnchorDate,
		IntervalDays:    v.IntervalDays,
		NextRunDate:     v.NextRunDate,
		Paused:          v.Paused,
		Version:         v.Version,
		CreatedAt:       v.CreatedAt,
		UpdatedAt:       v.UpdatedAt,
		DeletedAt:       v.DeletedAt,
		PreviewDates:    v.PreviewDates,
	}
}

func ruleResponseFromResultPtr(v *applicationrecurrence.RuleResult) *RuleResponse {
	if v == nil {
		return nil
	}

	out := ruleResponseFromResult(*v)

	return &out
}

func ruleResponseFromResultList(v []applicationrecurrence.RuleResult) []RuleResponse {
	if v == nil {
		return nil
	}

	out := make([]RuleResponse, len(v))
	for i := range v {
		out[i] = ruleResponseFromResult(v[i])
	}

	return out
}
