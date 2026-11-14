package infraconfig

import (
	"errors"
	"io"
	"os"
	"strings"

	infracache "github.com/pluto-han/taskflow/internal/infra/cache"
)

// Cache does not read the secret or validate unused fields while disabled.
func (c Config) Cache() (infracache.Options, error) {
	var options infracache.Options
	switch c.CacheEnabled {
	case "", "false":
		return options, nil
	case "true":
	default:
		return options, errors.New("TASKFLOW_CACHE_ENABLED must be true or false")
	}
	if err := infracache.ValidateEndpoint(c.RedisAddr, c.RedisUsername); err != nil {
		return options, err
	}
	if c.RedisPasswordFile == "" {
		return options, errors.New("REDIS_PASSWORD_FILE is required when cache is enabled")
	}

	file, err := os.Open(c.RedisPasswordFile)
	if err != nil {
		return options, errors.New("cannot read Redis password file")
	}

	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return options, errors.New("Redis password must be a regular file")
	}

	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(raw) > 4096 {
		return options, errors.New("cannot read Redis password file")
	}

	password := strings.TrimSpace(string(raw))
	if password == "" || strings.ContainsAny(password, " \t\r\n\x00") {
		return options, errors.New("invalid Redis password file")
	}

	return infracache.Options{
		Enabled:  true,
		Addr:     c.RedisAddr,
		Username: c.RedisUsername,
		Password: password,
	}, nil
}
