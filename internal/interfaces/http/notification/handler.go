package interfaceshttpnotification

import (
	"github.com/gin-gonic/gin"
	applicationnotification "github.com/pluto-han/taskflow/internal/application/notification"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
)

type Handler struct {
	service *applicationnotification.Service
}

func New(service *applicationnotification.Service) *Handler { return &Handler{service: service} }

func (h *Handler) List(c *gin.Context) {
	principal, ok := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if !ok {
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
		return
	}
	if h.service == nil {
		httpapi.WriteError(c, 503, "DISCORD_DISABLED", "Discord integration is not configured")
		return
	}

	events, e := h.service.List(c.Request.Context(), string(principal.UserID))
	if e != nil {
		httpapi.InternalError(c)
		return
	}

	c.JSON(200, eventResponseFromResultList(events))
}

func eventResponseFromResult(v applicationnotification.Event) EventResponse {
	return EventResponse{
		ID:            v.ID,
		RuleID:        v.RuleID,
		TaskID:        v.TaskID,
		ScheduledDate: v.ScheduledDate,
		Title:         v.Title,
		Status:        v.Status,
		Attempts:      v.Attempts,
		LastError:     v.LastError,
		CreatedAt:     v.CreatedAt,
	}
}

func eventResponseFromResultList(v []applicationnotification.Event) []EventResponse {
	if v == nil {
		return nil
	}

	out := make([]EventResponse, len(v))
	for i := range v {
		out[i] = eventResponseFromResult(v[i])
	}

	return out
}
