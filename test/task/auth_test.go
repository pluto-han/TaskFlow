package task_test

import (
	"context"
	"crypto/sha256"

	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainsession "github.com/pluto-han/taskflow/internal/domain/session"
)

const testOwner domainaccount.UserID = "00000000-0000-4000-8000-000000000001"
const otherOwner domainaccount.UserID = "00000000-0000-4000-8000-000000000002"
const testToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const otherToken = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"

type testSessionRepository struct{ domainsession.Repository }

func (testSessionRepository) FindActive(_ context.Context, hash [32]byte) (domainsession.Principal, error) {
	switch hash {
	case sha256.Sum256([]byte(testToken)):
		return domainsession.Principal{UserID: testOwner}, nil
	case sha256.Sum256([]byte(otherToken)):
		return domainsession.Principal{UserID: otherOwner}, nil
	default:
		return domainsession.Principal{}, domainaccount.ErrUnauthorized
	}
}

type testPasswords struct{ applicationsession.Passwords }

func (testPasswords) HashPassword(context.Context, string) (string, error) { return "test-dummy", nil }
