package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pluto-han/taskflow/internal/user"
)

var (
	ErrSessionNotFound       = errors.New("session not found")
	ErrInvalidSessionQuery   = errors.New("invalid session query")
	ErrInvalidPasswordChange = errors.New("invalid password change")
	ErrCurrentPassword       = errors.New("incorrect current password")
	ErrCredentialsChanged    = errors.New("credentials changed")
)

type SessionInfo struct {
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	CredentialKind string    `json:"credential_kind"`
	Current        bool      `json:"current"`
}
type SessionPage struct {
	Items      []SessionInfo `json:"items"`
	NextCursor string        `json:"next_cursor"`
}
type ListQuery struct {
	Limit int
	After *sessionCursor
}
type sessionCursor struct {
	Owner     UserID    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func sessionID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if len(raw) != 36 || id.Scan(raw) != nil || !id.Valid {
		return id, ErrInvalidSessionQuery
	}
	return id, nil
}

func parseSessions(raw string, owner UserID) (ListQuery, error) {
	query := ListQuery{Limit: 20}
	params, err := url.ParseQuery(raw)
	if err != nil {
		return query, ErrInvalidSessionQuery
	}
	for name, values := range params {
		if len(values) != 1 || values[0] == "" || (name != "limit" && name != "cursor") {
			return query, ErrInvalidSessionQuery
		}
	}
	if value := params.Get("limit"); value != "" {
		query.Limit, err = strconv.Atoi(value)
		if err != nil || query.Limit < 1 || query.Limit > 100 {
			return query, ErrInvalidSessionQuery
		}
	}
	if value := params.Get("cursor"); value != "" {
		if len(value) > 512 {
			return query, ErrInvalidSessionQuery
		}
		raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
		if err != nil {
			return query, ErrInvalidSessionQuery
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var cursor sessionCursor
		if decoder.Decode(&cursor) != nil || decoder.Decode(new(any)) != io.EOF || cursor.Owner != owner || cursor.CreatedAt.IsZero() || cursor.CreatedAt.Year() < 1 || cursor.CreatedAt.Year() > 9999 {
			return query, ErrInvalidSessionQuery
		}
		if _, err := sessionID(cursor.ID); err != nil {
			return query, err
		}
		query.After = &cursor
	}
	return query, nil
}

func (s *Service) ListSessions(ctx context.Context, p Principal, rawQuery string) (SessionPage, error) {
	query, err := parseSessions(rawQuery, p.UserID)
	if err != nil {
		return SessionPage{}, err
	}
	rows, err := s.sessions.ListActive(ctx, p.UserID, query)
	if err != nil {
		return SessionPage{}, err
	}
	page := SessionPage{Items: rows}
	if page.Items == nil {
		page.Items = []SessionInfo{}
	}
	if len(rows) > query.Limit {
		page.Items = rows[:query.Limit]
		last := page.Items[len(page.Items)-1]
		raw, _ := json.Marshal(sessionCursor{Owner: p.UserID, CreatedAt: last.CreatedAt, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	for i := range page.Items {
		page.Items[i].Current = page.Items[i].ID == p.SessionID
	}
	return page, nil
}

func (s *Service) RevokeSession(ctx context.Context, p Principal, rawID string) error {
	id, err := sessionID(rawID)
	if err != nil {
		return err
	}
	return s.sessions.RevokeOwned(ctx, p, id.String())
}

type PasswordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Service) ChangePassword(ctx context.Context, p Principal, input PasswordChangeRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !user.ValidPassword(input.NewPassword) || input.CurrentPassword == "" || len(input.CurrentPassword) > 512 || !utf8.ValidString(input.CurrentPassword) {
		return ErrInvalidPasswordChange
	}
	u, err := s.users.GetByID(ctx, string(p.UserID))
	if errors.Is(err, user.ErrUserNotFound) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	ok, err := s.passwords.VerifyPassword(ctx, input.CurrentPassword, u.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCurrentPassword
	}
	hash, err := s.passwords.HashPassword(ctx, input.NewPassword)
	if err != nil {
		return err
	}
	// Hashing is finished before acquiring any transaction/row lock.
	return s.sessions.ChangePassword(ctx, p, u.CredentialsVersion, hash)
}
