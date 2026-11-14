package applicationproject

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
)

func randomID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}

	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128

	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

func (s *Service) sharing(
	ctx context.Context,
	actor domainaccount.UserID,
	raw string,
) (domainproject.SharingRepository, string, error) {
	if err := check(ctx, actor); err != nil {
		return nil, "", err
	}

	id, err := domainproject.ParseID(raw)
	if err != nil {
		return nil, "", err
	}

	repo := s.shares
	if repo == nil {
		return nil, "", fmt.Errorf("sharing repository unavailable")
	}

	return repo, id, nil
}

func (s *Service) Members(
	ctx context.Context,
	a domainaccount.UserID,
	id string,
) ([]domainproject.Member, error) {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return nil, e
	}

	return r.Members(ctx, a, id)
}

func (s *Service) Invitations(
	ctx context.Context,
	a domainaccount.UserID,
	id string,
) ([]domainproject.Invitation, error) {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return nil, e
	}

	return r.Invitations(ctx, a, id)
}

func (s *Service) Invite(
	ctx context.Context,
	a domainaccount.UserID,
	id string,
	input InviteInput,
) (*domainproject.Invitation, error) {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return nil, e
	}

	recipient, e := domainproject.ParseID(input.UserID)
	if e != nil {
		return nil, domainproject.ErrInvalid
	}

	var secret [32]byte
	if _, e = rand.Read(secret[:]); e != nil {
		return nil, e
	}

	token := base64.RawURLEncoding.EncodeToString(secret[:])
	iid, e := randomID()
	if e != nil {
		return nil, e
	}

	result, e := r.Invite(ctx, a, id, recipient, iid, sha256.Sum256([]byte(token)))
	if e == nil {
		result.Token = token
	}
	return result, e
}

func (s *Service) RevokeInvitation(
	ctx context.Context,
	a domainaccount.UserID,
	id, iid string,
) error {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return e
	}

	iid, e = domainproject.ParseID(iid)
	if e != nil {
		return domainproject.ErrInvalid
	}

	return r.RevokeInvitation(ctx, a, id, iid)
}

func (s *Service) AcceptInvitation(
	ctx context.Context,
	a domainaccount.UserID,
	input AcceptInput,
) (string, error) {
	if e := check(ctx, a); e != nil {
		return "", e
	}

	raw, e := base64.RawURLEncoding.Strict().DecodeString(input.Token)
	if e != nil || len(raw) != 32 || len(input.Token) != 43 {
		return "", domainproject.ErrInvitationInvalid
	}

	r := s.shares
	if r == nil {
		return "", fmt.Errorf("sharing repository unavailable")
	}

	return r.AcceptInvitation(ctx, a, sha256.Sum256([]byte(input.Token)))
}

func (s *Service) RemoveMember(
	ctx context.Context,
	a domainaccount.UserID,
	id, member string,
	leave bool,
) error {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return e
	}
	if leave {
		member = string(a)
	}
	member, e = domainproject.ParseID(member)
	if e != nil {
		return domainproject.ErrInvalid
	}

	return r.RemoveMember(ctx, a, id, member, leave)
}

func (s *Service) Transfer(
	ctx context.Context,
	a domainaccount.UserID,
	id string,
	input InviteInput,
) error {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return e
	}

	target, e := domainproject.ParseID(input.UserID)
	if e != nil {
		return domainproject.ErrInvalid
	}

	return r.Transfer(ctx, a, id, target)
}
