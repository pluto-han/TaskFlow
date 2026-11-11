package config

import (
	"errors"
	"github.com/pluto-han/taskflow/internal/mail"
)

// Mail is off by default. No provider secret is read, even if unused variables
// are present. A real adapter and its secret-file contract require provider selection.
func (c Config) Mail() (mail.Sender, error) {
	switch c.MailEnabled {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errors.New("TASKFLOW_MAIL_ENABLED must be true or false")
	}
	if c.Environment != "development" || c.MailDriver != "memory" {
		return nil, errors.New("no production mail provider configured; development supports TASKFLOW_MAIL_DRIVER=memory only")
	}
	return &mail.MemorySender{}, nil
}
