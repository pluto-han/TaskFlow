package applicationsession

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"time"
	"unicode/utf8"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
)

func sessionQuery(input ListInput, owner domainaccount.UserID) (domainsession.ListQuery, error) {
	query := domainsession.ListQuery{Limit: input.Limit}
	if query.Limit == 0 {
		query.Limit = 20
	}
	if query.Limit < 1 || query.Limit > 100 {
		return query, domainsession.ErrInvalidSessionQuery
	}

	if value := input.Cursor; value != "" {
		if len(value) > 512 {
			return query, domainsession.ErrInvalidSessionQuery
		}

		raw, err := base64.RawURLEncoding.Strict().DecodeString(value)
		if err != nil {
			return query, domainsession.ErrInvalidSessionQuery
		}

		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var cursor sessionCursor
		if decoder.Decode(&cursor) != nil || decoder.Decode(new(any)) != io.EOF || cursor.Owner != owner || cursor.CreatedAt.IsZero() || cursor.CreatedAt.Year() < 1 || cursor.CreatedAt.Year() > 9999 {
			return query, domainsession.ErrInvalidSessionQuery
		}
		if _, err := domainsession.ParseID(cursor.ID); err != nil {
			return query, err
		}

		position := domainsession.Cursor(cursor)
		query.After = &position
	}
	return query, nil
}

func (s *Service) ListSessions(
	ctx context.Context,
	p domainsession.Principal,
	input ListInput,
) (SessionPageResult, error) {
	query, err := sessionQuery(input, p.UserID)
	if err != nil {
		return SessionPageResult{}, err
	}

	rows, err := s.sessions.ListActive(ctx, p.UserID, query)
	if err != nil {
		return SessionPageResult{}, err
	}

	items := make([]SessionInfoResult, len(rows))
	for i, row := range rows {
		items[i] = SessionInfoResult{
			ID:             row.ID,
			CreatedAt:      row.CreatedAt,
			ExpiresAt:      row.ExpiresAt,
			CredentialKind: row.CredentialKind,
			Current:        row.ID == p.SessionID,
		}
	}
	page := SessionPageResult{Items: items}
	if page.Items == nil {
		page.Items = []SessionInfoResult{}
	}
	if len(rows) > query.Limit {
		page.Items = items[:query.Limit]
		last := page.Items[len(page.Items)-1]
		raw, _ := json.Marshal(sessionCursor{Owner: p.UserID, CreatedAt: last.CreatedAt, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	for i := range page.Items {
		page.Items[i].Current = page.Items[i].ID == p.SessionID
	}

	return page, nil
}

func (s *Service) RevokeSession(
	ctx context.Context,
	p domainsession.Principal,
	rawID string,
) error {
	id, err := domainsession.ParseID(rawID)
	if err != nil {
		return err
	}

	return s.sessions.RevokeOwned(ctx, p, id)
}

func (s *Service) ChangePassword(
	ctx context.Context,
	p domainsession.Principal,
	input PasswordChangeInput,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !domainaccount.ValidPassword(input.NewPassword) || input.CurrentPassword == "" || len(input.CurrentPassword) > 512 || !utf8.ValidString(input.CurrentPassword) {
		return domainsession.ErrInvalidPasswordChange
	}

	u, err := s.users.GetByID(ctx, string(p.UserID))
	if errors.Is(err, domainaccount.ErrUserNotFound) {
		return domainaccount.ErrUnauthorized
	}
	if err != nil {
		return err
	}

	ok, err := s.passwords.VerifyPassword(ctx, input.CurrentPassword, u.PasswordHash)
	if err != nil {
		return err
	}
	if !ok {
		return domainsession.ErrCurrentPassword
	}

	hash, err := s.passwords.HashPassword(ctx, input.NewPassword)
	if err != nil {
		return err
	}

	// Hashing is finished before acquiring any transaction/row lock.
	return s.sessions.ChangePassword(ctx, p, u.CredentialsVersion, hash)
}

type sessionCursor struct {
	Owner     domainaccount.UserID `json:"owner"`
	CreatedAt time.Time            `json:"created_at"`
	ID        string               `json:"id"`
}
