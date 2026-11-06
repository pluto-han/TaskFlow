package config

import (
	"strings"
	"testing"
)

func TestDiscordConfig(t *testing.T) {
	if key, app, e := (Config{}).Discord(); e != nil || key != nil || app != "" {
		t.Fatal("default not disabled")
	}
	for _, c := range []Config{
		{DiscordApplicationID: "123"}, {DiscordPublicKey: strings.Repeat("ab", 32)},
		{DiscordApplicationID: "invalid", DiscordPublicKey: strings.Repeat("ab", 32)},
		{DiscordApplicationID: "123", DiscordPublicKey: "not-a-key"},
	} {
		if _, _, e := c.Discord(); e == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if key, _, e := (Config{DiscordApplicationID: "123", DiscordPublicKey: strings.Repeat("ab", 32)}).Discord(); e != nil || len(key) != 32 {
		t.Fatal(e)
	}
}
