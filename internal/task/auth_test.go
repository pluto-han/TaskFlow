package task

import (
	"context"
	"crypto/sha256"

	"github.com/pluto-han/taskflow/internal/session"
)

const testOwner session.UserID = "00000000-0000-4000-8000-000000000001"
const otherOwner session.UserID = "00000000-0000-4000-8000-000000000002"
const testToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const otherToken = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"

type testSessionRepository struct{ session.Repository }

func (testSessionRepository) FindActive(_ context.Context, hash [32]byte) (session.Principal, error) {
	switch hash {
	case sha256.Sum256([]byte(testToken)):
		return session.Principal{UserID: testOwner}, nil
	case sha256.Sum256([]byte(otherToken)):
		return session.Principal{UserID: otherOwner}, nil
	default:
		return session.Principal{}, session.ErrUnauthorized
	}
}

type testPasswords struct{ session.Passwords }

func (testPasswords) HashPassword(context.Context, string) (string, error) { return "test-dummy", nil }
