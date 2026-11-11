package config

import "testing"

func TestMailConfiguration(t *testing.T) {
	t.Setenv("TASKFLOW_MAIL_SECRET_FILE", "/does/not/exist")
	for _, enabled := range []string{"", "false"} {
		s, err := (Config{MailEnabled: enabled, MailDriver: "invalid", Environment: "production"}).Mail()
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
