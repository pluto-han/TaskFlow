package project

import (
	"github.com/pluto-han/taskflow/internal/session"
	"time"
)

type Project struct {
	Role        string         `json:"role"`
	ID          string         `json:"id"`
	OwnerID     session.UserID `json:"-"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type CreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type UpdateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}
