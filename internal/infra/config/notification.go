package infraconfig

import (
	"errors"
	"io"
	"os"
	"strings"
)

func (c *Config) NotificationToken() (bool, error) {
	switch c.DiscordSender {
	case "", "false":
		return false, nil
	case "true":
	default:
		return false, errors.New("TASKFLOW_DISCORD_SENDER_ENABLED must be true or false")
	}
	path := os.Getenv("DISCORD_BOT_TOKEN_FILE")
	if path == "" {
		return false, errors.New("DISCORD_BOT_TOKEN_FILE is required when sender is enabled")
	}

	file, e := os.Open(path)
	if e != nil {
		return false, errors.New("cannot read Discord bot token")
	}

	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return false, errors.New("Discord bot token must be a regular file")
	}

	buf, e := io.ReadAll(io.LimitReader(file, 4097))
	if e != nil || len(buf) > 4096 {
		return false, errors.New("cannot read Discord bot token")
	}

	token := strings.TrimSpace(string(buf))
	if len(token) == 0 || strings.ContainsAny(token, " \t\r\n") {
		return false, errors.New("invalid Discord bot token file")
	}

	c.DiscordBotToken = token

	return true, nil
}
