package mail_test

import (
	"context"
	"strings"
	"testing"

	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	inframail "github.com/pluto-han/taskflow/internal/infra/mail"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
)

func TestResetValidationAndDisabledMode(t *testing.T) {
	ctx := context.Background()
	disabled, _ := applicationaccount.NewCapabilities(nil, false, "", infrapassword.PasswordHasher{})
	if disabled.RequestReset(ctx, applicationaccount.ResetInput{Email: "not an email"}) != domainaccount.ErrDisabled || disabled.ConfirmReset(ctx, applicationaccount.ResetConfirmInput{}) != domainaccount.ErrDisabled {
		t.Fatal("disabled reset inspected input or database")
	}
	s, _ := applicationaccount.NewCapabilities(nil, &inframail.MemorySender{} != nil, "https://taskflow.example.com", infrapassword.PasswordHasher{})
	if s.RequestReset(ctx, applicationaccount.ResetInput{Email: "not an email"}) != domainaccount.ErrResetEmail {
		t.Fatal("invalid email reached database")
	}
	if s.ConfirmReset(ctx, applicationaccount.ResetConfirmInput{Token: "wrong", NewPassword: "valid password"}) != domainaccount.ErrResetToken {
		t.Fatal("invalid capability reached database")
	}
	for _, password := range []string{"short", "        ", strings.Repeat("x", 129)} {
		if s.ConfirmReset(ctx, applicationaccount.ResetConfirmInput{Token: strings.Repeat("A", 43), NewPassword: password}) != domainaccount.ErrResetPassword {
			t.Fatal("invalid password reached database")
		}
	}
}
