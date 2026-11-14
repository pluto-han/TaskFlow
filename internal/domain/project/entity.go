package domainproject

import (
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type Project struct {
	ID          string
	OwnerID     domainaccount.UserID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Change struct {
	Name        *string
	Description *string
}

// ParseID accepts a UUID and returns its canonical text representation.
func ParseID(raw string) (string, error) {

	compact := raw
	if len(raw) == 36 {
		compact = raw[:8] + raw[9:13] + raw[14:18] + raw[19:23] + raw[24:]
	} else if len(raw) != 32 {
		return "", ErrInvalid
	}
	if _, err := hex.DecodeString(compact); err != nil {
		return "", ErrInvalid
	}

	compact = strings.ToLower(compact)

	return compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:], nil

}

func ValidName(name string) bool {
	return utf8.ValidString(name) && len(strings.TrimSpace(name)) > 0 && utf8.RuneCountInString(name) <= 100 && !strings.ContainsRune(name, 0)
}

func ValidDescription(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= 2000 && !strings.ContainsRune(value, 0)
}

func NewProject(owner domainaccount.UserID, name, description string) (*Project, error) {
	name = strings.TrimSpace(name)
	if !ValidName(name) || !ValidDescription(description) {
		return nil, ErrInvalid
	}

	return &Project{OwnerID: owner, Name: name, Description: description}, nil
}

func (c Change) Normalize() (Change, error) {
	if c.Name == nil && c.Description == nil {
		return c, ErrInvalid
	}
	if c.Name != nil {
		value := strings.TrimSpace(*c.Name)
		if !ValidName(value) {
			return c, ErrInvalid
		}

		c.Name = &value
	}
	if c.Description != nil && !ValidDescription(*c.Description) {
		return c, ErrInvalid
	}

	return c, nil
}

type Member struct {
	UserID string
	Role   string
}
type Invitation struct {
	ID            string
	ProjectID     string
	InviteeUserID string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	AcceptedAt    *time.Time
	Token         string
}
