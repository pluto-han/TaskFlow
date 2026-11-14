package config_test

import (
	"os"
	"path/filepath"
	"testing"

	infraconfig "github.com/pluto-han/taskflow/internal/infra/config"
)

func TestDatabaseSecretFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte("postgres://example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL_FILE", path)
	c := infraconfig.Config{}
	if err := c.ReadSecrets(); err != nil || c.DatabaseURL != "postgres://example" {
		t.Fatal("file not loaded")
	}
	if c.ReadSecrets() == nil {
		t.Fatal("ambiguous sources accepted")
	}
	t.Setenv("DATABASE_URL_FILE", path+"-absent")
	c = infraconfig.Config{}
	if c.ReadSecrets() == nil {
		t.Fatal("missing file accepted")
	}
}
