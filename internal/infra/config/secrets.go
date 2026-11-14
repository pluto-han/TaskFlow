package infraconfig

import (
	"errors"
	"os"
	"strings"
)

// ReadSecrets supports Compose secret files without putting credentials into
// image layers or container environment values. Ambiguous sources fail closed.
func (c *Config) ReadSecrets() error {
	path := os.Getenv("DATABASE_URL_FILE")
	if path == "" {
		return nil
	}
	if c.DatabaseURL != "" {
		return errors.New("set only DATABASE_URL or DATABASE_URL_FILE")
	}

	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 16384 || len(strings.TrimSpace(string(raw))) == 0 {
		return errors.New("cannot read database URL secret")
	}

	c.DatabaseURL = strings.TrimSpace(string(raw))

	return nil
}
