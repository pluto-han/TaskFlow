package config

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/pluto-han/taskflow/internal/mail"
)

// Mail is off by default. No provider secret is read, even if unused variables
// are present. Production credentials are accepted only through a secret file.
func (c Config) Mail() (mail.Sender, error) {
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
		return &mail.MemorySender{}, nil
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
		return mail.NewResendSender(strings.TrimSpace(string(raw)), c.MailFrom, nil)
	default:
		return nil, errors.New("TASKFLOW_MAIL_DRIVER must be memory or resend")
	}
}
