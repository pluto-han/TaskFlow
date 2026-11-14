package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	infraconfig "github.com/pluto-han/taskflow/internal/infra/config"
)

func TestCacheDisabledDoesNotReadSecrets(t *testing.T) {
	for _, flag := range []string{"", "false"} {
		options, err := (infraconfig.Config{CacheEnabled: flag, RedisAddr: "invalid", RedisPasswordFile: "/missing/private-secret"}).Cache()
		if err != nil || options.Enabled || options.Password != "" {
			t.Fatal("disabled cache read or validated unused credentials")
		}
	}
}

func TestCacheConfigValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("fixture-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	valid := infraconfig.Config{CacheEnabled: "true", RedisAddr: "redis:6379", RedisUsername: "taskflow_cache", RedisPasswordFile: path}
	options, err := valid.Cache()
	if err != nil || !options.Enabled || options.Password != "fixture-secret" {
		t.Fatal("valid credentials rejected")
	}
	for _, tc := range []struct {
		name   string
		change func(*infraconfig.Config)
	}{
		{"bad flag", func(c *infraconfig.Config) { c.CacheEnabled = "TRUE" }},
		{"missing address", func(c *infraconfig.Config) { c.RedisAddr = "" }},
		{"inline credentials", func(c *infraconfig.Config) { c.RedisAddr = "redis://private-secret@redis:6379" }},
		{"invalid port", func(c *infraconfig.Config) { c.RedisAddr = "redis:70000" }},
		{"missing user", func(c *infraconfig.Config) { c.RedisUsername = "" }},
		{"default user", func(c *infraconfig.Config) { c.RedisUsername = "default" }},
		{"missing file", func(c *infraconfig.Config) { c.RedisPasswordFile = "" }},
		{"unreadable file", func(c *infraconfig.Config) { c.RedisPasswordFile += "/private-secret" }},
		{"directory", func(c *infraconfig.Config) { c.RedisPasswordFile = filepath.Dir(path) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := valid
			tc.change(&c)
			options, err := c.Cache()
			if err == nil || options.Enabled || options.Password != "" || strings.Contains(err.Error(), "private-secret") {
				t.Fatal("invalid configuration accepted or error leaked credentials")
			}
		})
	}
	for _, value := range []string{"", "   \n", "two words", strings.Repeat("x", 4097)} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if options, err := valid.Cache(); err == nil || options.Enabled || options.Password != "" {
			t.Fatal("bad secret accepted")
		}
	}
}

func TestLoadCacheEnvironment(t *testing.T) {
	t.Setenv("TASKFLOW_CACHE_ENABLED", "false")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("REDIS_USERNAME", "taskflow_cache")
	t.Setenv("REDIS_PASSWORD_FILE", "/unused/password")
	c := infraconfig.Load()
	if c.CacheEnabled != "false" || c.RedisAddr != "redis:6379" || c.RedisUsername != "taskflow_cache" || c.RedisPasswordFile != "/unused/password" {
		t.Fatal("cache environment not loaded")
	}
}
