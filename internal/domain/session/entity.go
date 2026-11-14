package domainsession

import (
	"encoding/hex"
	"strings"
	"time"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
)

type SessionInfo struct {
	ID             string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	CredentialKind string
}

type ListQuery struct {
	Limit int
	After *Cursor
}

type Cursor struct {
	Owner     domainaccount.UserID
	CreatedAt time.Time
	ID        string
}

type Principal struct {
	SessionID string
	UserID    domainaccount.UserID
	ExpiresAt time.Time
}

type BrowserPrincipal struct {
	Principal
	CSRFSecret [32]byte
}

// ParseID preserves the session UUID syntax and canonical form without a driver dependency.
func ParseID(raw string) (string, error) {
	if len(raw) != 36 {
		return "", ErrInvalidSessionQuery
	}

	compact := raw[:8] + raw[9:13] + raw[14:18] + raw[19:23] + raw[24:]
	if _, err := hex.DecodeString(compact); err != nil {
		return "", ErrInvalidSessionQuery
	}

	compact = strings.ToLower(compact)

	return compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:], nil
}
