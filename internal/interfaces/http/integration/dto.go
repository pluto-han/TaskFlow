package interfaceshttpintegration

import (
	"time"
)

type BindingResponse struct {
	Enabled     bool       `json:"enabled"`
	Version     int64      `json:"version"`
	Bound       bool       `json:"bound"`
	GuildID     *string    `json:"guild_id"`
	ChannelID   *string    `json:"channel_id"`
	ChannelName *string    `json:"channel_name"`
	BoundAt     *time.Time `json:"bound_at"`
}

type CodeResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type UnbindRequest struct {
	Version int64 `json:"version"`
}
