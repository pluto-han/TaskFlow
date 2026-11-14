package interfaceshttpproject

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	applicationproject "github.com/pluto-han/taskflow/internal/application/project"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
	"github.com/pluto-han/taskflow/internal/httpapi"
	interfaceshttpmiddleware "github.com/pluto-han/taskflow/internal/interfaces/http/middleware"
)

type Handler struct{ service *applicationproject.Service }

func New(service *applicationproject.Service) *Handler { return &Handler{service: service} }

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domainaccount.ErrUnauthorized):
		c.Header("WWW-Authenticate", "Bearer")
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(err, domainproject.ErrForbidden):
		httpapi.WriteError(c, 403, "FORBIDDEN", "Operation not permitted")
	case errors.Is(err, domainproject.ErrInvitationInvalid):
		httpapi.WriteError(c, 400, "INVITATION_INVALID", "Invitation is invalid or unavailable")
	case errors.Is(err, domainproject.ErrInvalidInvitee):
		httpapi.WriteError(c, 400, "INVALID_INVITEE", "Recipient or ownership target is invalid")
	case errors.Is(err, domainproject.ErrOwnerRequired):
		httpapi.WriteError(c, 409, "OWNER_REQUIRED", "Transfer ownership before leaving")
	case errors.Is(err, domainproject.ErrInvalid):
		httpapi.WriteError(c, 400, "INVALID_PROJECT", "Invalid project data")
	case errors.Is(err, domainproject.ErrNotFound):
		httpapi.WriteError(c, 404, "PROJECT_NOT_FOUND", "Project not found")
	case errors.Is(err, domainproject.ErrNotEmpty):
		httpapi.WriteError(c, 409, "PROJECT_NOT_EMPTY", "Move or delete tasks before deleting the project")
	case errors.Is(err, context.Canceled):
	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, 504, "REQUEST_TIMEOUT", "Request timed out")
	default:
		httpapi.InternalError(c)
	}
}

func owner(c *gin.Context) (domainaccount.UserID, bool) {
	p, ok := interfaceshttpmiddleware.CurrentPrincipal(c.Request.Context())
	if !ok || p.UserID == "" {
		writeError(c, domainaccount.ErrUnauthorized)
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

	result, err := h.service.Create(c.Request.Context(), uid, input.input())
	if err != nil {
		writeError(c, err)
		return
	}

	c.Header("Location", "/projects/"+result.ID)
	c.JSON(201, projectResponseFromResultPtr(result))
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

	c.JSON(200, projectResponseFromResultPtr(result))
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

	c.JSON(200, projectResponseFromResultList(result))
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

	result, err := h.service.Update(c.Request.Context(), uid, c.Param("id"), input.input())
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(200, projectResponseFromResultPtr(result))
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

func (v CreateRequest) input() applicationproject.CreateInput {
	return applicationproject.CreateInput{
		Name:        v.Name,
		Description: v.Description,
	}
}

func (v InviteRequest) input() applicationproject.InviteInput {
	return applicationproject.InviteInput{
		UserID: v.UserID,
	}
}

func (v AcceptRequest) input() applicationproject.AcceptInput {
	return applicationproject.AcceptInput{
		Token: v.Token,
	}
}

func (v UpdateRequest) input() domainproject.Change {
	return domainproject.Change{
		Name:        v.Name,
		Description: v.Description,
	}
}

func projectResponseFromResult(v applicationproject.ProjectResult) ProjectResponse {
	return ProjectResponse{
		Role:        v.Role,
		ID:          v.ID,
		OwnerID:     v.OwnerID,
		Name:        v.Name,
		Description: v.Description,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
	}
}

func projectResponseFromResultPtr(v *applicationproject.ProjectResult) *ProjectResponse {
	if v == nil {
		return nil
	}

	out := projectResponseFromResult(*v)

	return &out
}

func projectResponseFromResultList(v []applicationproject.ProjectResult) []ProjectResponse {
	if v == nil {
		return nil
	}

	out := make([]ProjectResponse, len(v))
	for i := range v {
		out[i] = projectResponseFromResult(v[i])
	}

	return out
}

func memberResponseFromResult(v domainproject.Member) MemberResponse {
	return MemberResponse{
		UserID: v.UserID,
		Role:   v.Role,
	}
}

func memberResponseFromResultList(v []domainproject.Member) []MemberResponse {
	if v == nil {
		return nil
	}

	out := make([]MemberResponse, len(v))
	for i := range v {
		out[i] = memberResponseFromResult(v[i])
	}

	return out
}

func invitationResponseFromResult(v domainproject.Invitation) InvitationResponse {
	return InvitationResponse{
		ID:            v.ID,
		ProjectID:     v.ProjectID,
		InviteeUserID: v.InviteeUserID,
		CreatedAt:     v.CreatedAt,
		ExpiresAt:     v.ExpiresAt,
		RevokedAt:     v.RevokedAt,
		AcceptedAt:    v.AcceptedAt,
		Token:         v.Token,
	}
}

func invitationResponseFromResultPtr(v *domainproject.Invitation) *InvitationResponse {
	if v == nil {
		return nil
	}

	out := invitationResponseFromResult(*v)

	return &out
}

func invitationResponseFromResultList(v []domainproject.Invitation) []InvitationResponse {
	if v == nil {
		return nil
	}

	out := make([]InvitationResponse, len(v))
	for i := range v {
		out[i] = invitationResponseFromResult(v[i])
	}

	return out
}
