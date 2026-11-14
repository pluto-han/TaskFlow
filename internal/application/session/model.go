package applicationsession

import (
	"time"
)

// Profile contains the account fields exposed by session use cases.
type Profile struct {
	ID        string
	Email     string
	CreatedAt time.Time
}

type SessionPageResult struct {
	Items      []SessionInfoResult
	NextCursor string
}

type PasswordChangeInput struct {
	CurrentPassword string
	NewPassword     string
}

type LoginInput struct {
	Email    string
	Password string
}

type LoginResult struct {
	AccessToken string
	TokenType   string
	ExpiresAt   time.Time
}

type BrowserResult struct {
	User      Profile
	ExpiresAt time.Time
	CSRFToken string
}

type SessionInfoResult struct {
	ID             string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	CredentialKind string
	Current        bool
}

type ListInput struct {
	Limit  int
	Cursor string
}
