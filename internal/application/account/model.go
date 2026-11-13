package applicationaccount

import (
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type CreateInput struct {
	Email    string
	Password string
}

// Profile excludes credentials from application results.
type Profile struct {
	ID        string
	Email     string
	CreatedAt time.Time
}

func profileFromUser(u *domainaccount.User) *Profile {
	return &Profile{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}
}
