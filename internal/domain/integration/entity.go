package domainintegration

import (
	"strconv"
	"time"
)

type Binding struct {
	Enabled     bool
	Version     int64
	Bound       bool
	GuildID     *string
	ChannelID   *string
	ChannelName *string
	BoundAt     *time.Time
}

type Code struct {
	Code      string
	ExpiresAt time.Time
}

type Target struct{ InteractionID, GuildID, ChannelID, ChannelName, DiscordUserID string }

func Snowflake(s string) bool {
	if len(s) < 1 || len(s) > 20 || s[0] == '0' {
		return false
	}

	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	_, e := strconv.ParseUint(s, 10, 64)

	return e == nil
}
