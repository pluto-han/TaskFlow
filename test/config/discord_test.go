package config_test

import (
	"strings"
	"testing"

	infraconfig "github.com/pluto-han/taskflow/internal/infra/config"
)

func TestDiscordConfig(t *testing.T) {
	if key, app, e := (infraconfig.Config{}).Discord(); e != nil || key != nil || app != "" {
		t.Fatal("default not disabled")
	}
	for _, c := range []infraconfig.Config{
		{DiscordApplicationID: "123"}, {DiscordPublicKey: strings.Repeat("ab", 32)},
		{DiscordApplicationID: "invalid", DiscordPublicKey: strings.Repeat("ab", 32)},
		{DiscordApplicationID: "123", DiscordPublicKey: "not-a-key"},
	} {
		if _, _, e := c.Discord(); e == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if key, _, e := (infraconfig.Config{DiscordApplicationID: "123", DiscordPublicKey: strings.Repeat("ab", 32)}).Discord(); e != nil || len(key) != 32 {
		t.Fatal(e)
	}
}
