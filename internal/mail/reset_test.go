package mail

import (
	"context"
	"strings"
	"testing"
)

func TestResetValidationAndDisabledMode(t *testing.T) {
	ctx := context.Background()
	disabled, _ := New(nil, nil, "")
	if disabled.RequestReset(ctx, ResetRequest{Email: "not an email"}) != ErrDisabled || disabled.ConfirmReset(ctx, ResetConfirmRequest{}) != ErrDisabled {
		t.Fatal("disabled reset inspected input or database")
	}
	s, _ := New(nil, &MemorySender{}, "https://taskflow.example.com")
	if s.RequestReset(ctx, ResetRequest{Email: "not an email"}) != ErrResetEmail {
		t.Fatal("invalid email reached database")
	}
	if s.ConfirmReset(ctx, ResetConfirmRequest{Token: "wrong", NewPassword: "valid password"}) != ErrResetToken {
		t.Fatal("invalid capability reached database")
	}
	for _, password := range []string{"short", "        ", strings.Repeat("x", 129)} {
		if s.ConfirmReset(ctx, ResetConfirmRequest{Token: strings.Repeat("A", 43), NewPassword: password}) != ErrResetPassword {
			t.Fatal("invalid password reached database")
		}
	}
}
