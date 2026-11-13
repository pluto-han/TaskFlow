package session

import "time"

// UserResponse is the legacy session profile DTO until the session chain is migrated.
type UserResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}
