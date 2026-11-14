package interfaceshttpaccount

import (
	"time"
)

type ResetRequest struct {
	Email string `json:"email"`
}

type ResetConfirmRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type VerificationStatusResponse struct {
	Enabled    bool       `json:"enabled"`
	VerifiedAt *time.Time `json:"verified_at"`
}

type ConfirmRequest struct {
	Token string `json:"token"`
}
