package interfaceshttpproject

import (
	"github.com/gin-gonic/gin"
)

func (h *Handler) Members(c *gin.Context) {
	a, ok := owner(c)
	if !ok {
		return
	}

	result, e := h.service.Members(c.Request.Context(), a, c.Param("id"))
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(200, memberResponseFromResultList(result))
}

func (h *Handler) Invitations(c *gin.Context) {
	a, ok := owner(c)
	if !ok {
		return
	}

	result, e := h.service.Invitations(c.Request.Context(), a, c.Param("id"))
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(200, invitationResponseFromResultList(result))
}

func (h *Handler) Invite(c *gin.Context) {
	a, ok := owner(c)
	if !ok {
		return
	}

	var input InviteRequest
	if !decode(c, &input) {
		return
	}

	result, e := h.service.Invite(c.Request.Context(), a, c.Param("id"), input.input())
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(201, invitationResponseFromResultPtr(result))
}

func (h *Handler) RevokeInvitation(c *gin.Context) {
	a, ok := owner(c)
	if !ok {
		return
	}
	if e := h.service.RevokeInvitation(c.Request.Context(), a, c.Param("id"), c.Param("invitationID")); e != nil {
		writeError(c, e)
		return
	}

	c.Status(204)
}

func (h *Handler) AcceptInvitation(c *gin.Context) {
	a, ok := owner(c)
	if !ok {
		return
	}

	var input AcceptRequest
	if !decode(c, &input) {
		return
	}

	id, e := h.service.AcceptInvitation(c.Request.Context(), a, input.input())
	if e != nil {
		writeError(c, e)
		return
	}

	c.JSON(200, gin.H{"project_id": id})
}

func (h *Handler) RemoveMember(c *gin.Context) {
	a, ok := owner(c)
	if !ok {
		return
	}
	if e := h.service.RemoveMember(c.Request.Context(), a, c.Param("id"), c.Param("userID"), false); e != nil {
		writeError(c, e)
		return
	}

	c.Status(204)
}

func (h *Handler) Leave(c *gin.Context) {
	a, ok := owner(c)
	if !ok {
		return
	}
	if e := h.service.RemoveMember(c.Request.Context(), a, c.Param("id"), "", true); e != nil {
		writeError(c, e)
		return
	}

	c.Status(204)
}

func (h *Handler) Transfer(c *gin.Context) {
	a, ok := owner(c)
	if !ok {
		return
	}

	var input InviteRequest
	if !decode(c, &input) {
		return
	}
	if e := h.service.Transfer(c.Request.Context(), a, c.Param("id"), input.input()); e != nil {
		writeError(c, e)
		return
	}

	c.Status(204)
}
