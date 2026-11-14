package applicationproject

import (
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
)

type CreateInput struct {
	Name        string
	Description string
}

type InviteInput struct {
	UserID string
}

type AcceptInput struct {
	Token string
}

type ProjectResult struct {
	Role        string
	ID          string
	OwnerID     domainaccount.UserID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func projectResult(project *domainproject.Project, viewer domainaccount.UserID) *ProjectResult {
	if project == nil {
		return nil
	}

	role := "member"
	if project.OwnerID == viewer {
		role = "owner"
	}
	return &ProjectResult{
		Role:        role,
		ID:          project.ID,
		OwnerID:     project.OwnerID,
		Name:        project.Name,
		Description: project.Description,
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
	}
}
