package infrasession

import (
	"errors"
	"fmt"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
)

func sessionID(raw string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if len(raw) != 36 || id.Scan(raw) != nil || !id.Valid {
		return id, domainsession.ErrInvalidSessionQuery
	}

	return id, nil
}

func principalFromRow(
	id, uid pgtype.UUID,
	expiry pgtype.Timestamptz,
	err error,
) (domainsession.Principal, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return domainsession.Principal{}, domainaccount.ErrUnauthorized
	}
	if err != nil {
		return domainsession.Principal{}, fmt.Errorf("session repository: %w", err)
	}
	if !id.Valid || !uid.Valid || !expiry.Valid || expiry.InfinityModifier != pgtype.Finite {
		return domainsession.Principal{}, errors.New("invalid session mapping")
	}

	return domainsession.Principal{
		SessionID: id.String(),
		UserID:    domainaccount.UserID(uid.String()),
		ExpiresAt: expiry.Time.UTC(),
	}, nil
}
