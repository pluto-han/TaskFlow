package domainaccount

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

type UserID string

type User struct {
	ID                 string
	Email              string
	PasswordHash       string
	CredentialsVersion int64
	CreatedAt          time.Time
}

// NewUser validates registration input before any hashing or persistence.
func NewUser(email, password string) (*User, error) {
	normalized, valid := NormalizeEmail(email)
	if !valid || !ValidPassword(password) {
		return nil, ErrInvalidUser
	}

	return &User{Email: normalized}, nil
}

// RestoreUser preserves stored credential epochs and timestamps without registration validation.
func RestoreUser(
	id, email, passwordHash string,
	credentialsVersion int64,
	createdAt time.Time,
) *User {
	return &User{
		ID:                 id,
		Email:              email,
		PasswordHash:       passwordHash,
		CredentialsVersion: credentialsVersion,
		CreatedAt:          createdAt,
	}
}

// NormalizeEmail applies the shared registration/login identity policy.
func NormalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if len(email) < 3 || len(email) > 254 {
		return "", false
	}

	for _, c := range email {
		if c <= 32 || c >= 127 {
			return "", false
		}
	}

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || parsed.Name != "" {
		return "", false
	}

	// Restrict this lesson to common, unquoted ASCII mailbox addresses.
	local, domain, ok := strings.Cut(email, "@")
	if !ok ||
		len(local) > 64 ||
		strings.ContainsAny(local, "\"\\") ||
		!strings.Contains(domain, ".") {
		return "", false
	}

	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}

		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", false
			}
		}
	}

	return email, true
}

// ValidPassword is shared by registration and authenticated password changes.
func ValidPassword(password string) bool {
	count := utf8.RuneCountInString(password)

	return utf8.ValidString(password) &&
		count >= 8 &&
		count <= 128 &&
		len(password) <= 512 &&
		strings.TrimSpace(password) != ""
}
