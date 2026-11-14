package infraconfig

import (
	"errors"
	"io"
	"os"
	"strings"

	inframail "github.com/pluto-han/taskflow/internal/infra/mail"
)

// Mail is off by default. No provider secret is read, even if unused variables
// are present. Production credentials are accepted only through a secret file.
func (c Config) Mail() (*MailOptions, error) {
	switch c.MailEnabled {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errors.New("TASKFLOW_MAIL_ENABLED must be true or false")
	}
	switch c.MailDriver {
	case "memory":
		if c.Environment != "development" {
			return nil, errors.New("memory mail requires development mode")
		}
		return &MailOptions{Driver: "memory"}, nil
	case "resend":
		if c.Environment != "development" && c.Environment != "production" {
			return nil, errors.New("invalid mail environment")
		}
		file, err := os.Open(c.MailSecretFile)
		if err != nil {
			return nil, errors.New("cannot read mail secret file")
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, 4097))
		if err != nil || len(raw) > 4096 {
			return nil, errors.New("invalid mail secret file")
		}
		key := strings.TrimSpace(string(raw))
		if err := inframail.ValidateResend(key, c.MailFrom); err != nil {
			return nil, err
		}
		return &MailOptions{Driver: "resend", Key: key, From: c.MailFrom}, nil
	default:
		return nil, errors.New("TASKFLOW_MAIL_DRIVER must be memory or resend")
	}
}

type MailOptions struct {
	Driver string
	Key    string
	From   string
}
