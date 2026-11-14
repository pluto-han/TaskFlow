package interfaceshttpproject

import (
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type CreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type InviteRequest struct {
	UserID string `json:"user_id"`
}

type AcceptRequest struct {
	Token string `json:"token"`
}

type UpdateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type ProjectResponse struct {
	Role        string               `json:"role"`
	ID          string               `json:"id"`
	OwnerID     domainaccount.UserID `json:"-"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

type MemberResponse struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type InvitationResponse struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	InviteeUserID string     `json:"invitee_user_id"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at"`
	AcceptedAt    *time.Time `json:"accepted_at"`
	Token         string     `json:"token,omitempty"`
}
