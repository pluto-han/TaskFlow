package project

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/pluto-han/taskflow/internal/session"
)

type Member struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}
type Invitation struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	InviteeUserID string     `json:"invitee_user_id"`
	CreatedAt     time.Time  `json:"created_at"`
	ExpiresAt     time.Time  `json:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at"`
	AcceptedAt    *time.Time `json:"accepted_at"`
	Token         string     `json:"token,omitempty"`
}
type InviteRequest struct {
	UserID string `json:"user_id"`
}
type AcceptRequest struct {
	Token string `json:"token"`
}
type SharingRepository interface {
	Members(context.Context, session.UserID, string) ([]Member, error)
	Invite(context.Context, session.UserID, string, string, string, [32]byte) (*Invitation, error)
	Invitations(context.Context, session.UserID, string) ([]Invitation, error)
	RevokeInvitation(context.Context, session.UserID, string, string) error
	AcceptInvitation(context.Context, session.UserID, [32]byte) (string, error)
	RemoveMember(context.Context, session.UserID, string, string, bool) error
	Transfer(context.Context, session.UserID, string, string) error
}

func randomID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}
func (s *Service) sharing(ctx context.Context, actor session.UserID, raw string) (SharingRepository, string, error) {
	if err := check(ctx, actor); err != nil {
		return nil, "", err
	}
	id, err := ParseID(raw)
	if err != nil {
		return nil, "", err
	}
	repo, ok := s.repo.(SharingRepository)
	if !ok {
		return nil, "", fmt.Errorf("sharing repository unavailable")
	}
	return repo, id, nil
}
func (s *Service) Members(ctx context.Context, a session.UserID, id string) ([]Member, error) {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return nil, e
	}
	return r.Members(ctx, a, id)
}
func (s *Service) Invitations(ctx context.Context, a session.UserID, id string) ([]Invitation, error) {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return nil, e
	}
	return r.Invitations(ctx, a, id)
}
func (s *Service) Invite(ctx context.Context, a session.UserID, id string, input InviteRequest) (*Invitation, error) {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return nil, e
	}
	recipient, e := ParseID(input.UserID)
	if e != nil {
		return nil, ErrInvalid
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
func (s *Service) RevokeInvitation(ctx context.Context, a session.UserID, id, iid string) error {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return e
	}
	iid, e = ParseID(iid)
	if e != nil {
		return ErrInvalid
	}
	return r.RevokeInvitation(ctx, a, id, iid)
}
func (s *Service) AcceptInvitation(ctx context.Context, a session.UserID, input AcceptRequest) (string, error) {
	if e := check(ctx, a); e != nil {
		return "", e
	}
	raw, e := base64.RawURLEncoding.Strict().DecodeString(input.Token)
	if e != nil || len(raw) != 32 || len(input.Token) != 43 {
		return "", ErrInvitationInvalid
	}
	r, ok := s.repo.(SharingRepository)
	if !ok {
		return "", fmt.Errorf("sharing repository unavailable")
	}
	return r.AcceptInvitation(ctx, a, sha256.Sum256([]byte(input.Token)))
}
func (s *Service) RemoveMember(ctx context.Context, a session.UserID, id, member string, leave bool) error {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return e
	}
	if leave {
		member = string(a)
	}
	member, e = ParseID(member)
	if e != nil {
		return ErrInvalid
	}
	return r.RemoveMember(ctx, a, id, member, leave)
}
func (s *Service) Transfer(ctx context.Context, a session.UserID, id string, input InviteRequest) error {
	r, id, e := s.sharing(ctx, a, id)
	if e != nil {
		return e
	}
	target, e := ParseID(input.UserID)
	if e != nil {
		return ErrInvalid
	}
	return r.Transfer(ctx, a, id, target)
}
