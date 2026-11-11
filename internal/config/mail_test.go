package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMailConfiguration(t *testing.T) {
	t.Setenv("TASKFLOW_MAIL_SECRET_FILE", "/does/not/exist")
	for _, enabled := range []string{"", "false"} {
		s, err := (Config{MailEnabled: enabled, MailDriver: "resend", MailSecretFile: "/does/not/exist", Environment: "production"}).Mail()
		if err != nil || s != nil {
			t.Fatal("disabled mail used provider configuration")
		}
	}
	for _, c := range []Config{{MailEnabled: "yes"}, {MailEnabled: "true", MailDriver: "smtp", Environment: "development"}, {MailEnabled: "true", MailDriver: "memory", Environment: "production"}} {
		if _, err := c.Mail(); err == nil {
			t.Fatal("unsafe mail enabled")
		}
	}
	if s, err := (Config{MailEnabled: "true", MailDriver: "memory", Environment: "development"}).Mail(); err != nil || s == nil {
		t.Fatal("fake mail unavailable")
	}
}

func TestResendMailConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	base := Config{MailEnabled: "true", MailDriver: "resend", MailFrom: "TaskFlow <noreply@example.com>", MailSecretFile: path, Environment: "production"}
	if _, err := base.Mail(); err == nil {
		t.Fatal("missing secret accepted")
	}
	for _, value := range []string{"", "bad-secret", "re_bad\nsecret", strings.Repeat("x", 4097), "re_test\n"} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		sender, err := base.Mail()
		if value == "re_test\n" {
			if err != nil || sender == nil {
				t.Fatal("valid file configuration rejected")
			}
		} else if err == nil || strings.Contains(err.Error(), "bad-secret") {
			t.Fatal("unsafe secret accepted or exposed")
		}
	}
	t.Setenv("TASKFLOW_MAIL_DRIVER", "resend")
	t.Setenv("TASKFLOW_MAIL_FROM", base.MailFrom)
	t.Setenv("TASKFLOW_MAIL_SECRET_FILE", path)
	c := Load()
	if c.MailDriver != "resend" || c.MailFrom != base.MailFrom || c.MailSecretFile != path {
		t.Fatal("mail configuration not loaded")
	}
}
