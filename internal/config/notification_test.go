package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationTokenOptIn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if e := os.WriteFile(path, []byte("fixture-secret\n"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("DISCORD_BOT_TOKEN_FILE", path)
	c := Config{}
	on, e := c.NotificationToken()
	if e != nil || on || c.DiscordBotToken != "" {
		t.Fatal("default enabled")
	}
	c.DiscordSender = "true"
	on, e = c.NotificationToken()
	if e != nil || !on || c.DiscordBotToken != "fixture-secret" {
		t.Fatal("valid file rejected")
	}
	c.DiscordSender = "TRUE"
	if _, e = c.NotificationToken(); e == nil {
		t.Fatal("bad flag")
	}
	c.DiscordSender = "true"
	if e = os.WriteFile(path, []byte(strings.Repeat("x", 4097)), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = c.NotificationToken(); e == nil {
		t.Fatal("oversize token")
	}
}
