package interfaceshttpsession

import (
	"time"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type PasswordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type UserResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type LoginResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type BrowserResponse struct {
	User      UserResponse `json:"user"`
	ExpiresAt time.Time    `json:"expires_at"`
	CSRFToken string       `json:"csrf_token"`
}

type SessionPage struct {
	Items      []SessionInfoResponse `json:"items"`
	NextCursor string                `json:"next_cursor"`
}

type SessionInfoResponse struct {
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	CredentialKind string    `json:"credential_kind"`
	Current        bool      `json:"current"`
}
