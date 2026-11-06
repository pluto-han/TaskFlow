// Package discord verifies Discord's signed protocol; binding state lives in integration.
package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"math/big"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/pluto-han/taskflow/internal/integration"
)

const Path = "/discord/interactions"

type Option struct {
	Name    string   `json:"name"`
	Type    int      `json:"type"`
	Value   string   `json:"value,omitempty"`
	Options []Option `json:"options,omitempty"`
}
type Interaction struct {
	ID            string `json:"id"`
	ApplicationID string `json:"application_id"`
	Type          int    `json:"type"`
	GuildID       string `json:"guild_id"`
	ChannelID     string `json:"channel_id"`
	Channel       struct {
		ID   string `json:"id"`
		Type *int   `json:"type"`
		Name string `json:"name"`
	} `json:"channel"`
	Member struct {
		Permissions string `json:"permissions"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"member"`
	AppPermissions string            `json:"app_permissions"`
	Owners         map[string]string `json:"authorizing_integration_owners"`
	Data           struct {
		Name    string   `json:"name"`
		Type    int      `json:"type"`
		Options []Option `json:"options"`
	} `json:"data"`
}

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
func Verify(key ed25519.PublicKey, signature, timestamp string, raw []byte, now time.Time) bool {
	if len(key) != ed25519.PublicKeySize || len(signature) != 128 || len(timestamp) > 12 {
		return false
	}
	seconds, e := strconv.ParseInt(timestamp, 10, 64)
	if e != nil || seconds < 0 {
		return false
	}
	// Five-minute max age and thirty-second future skew are application policy.
	delta := now.Sub(time.Unix(seconds, 0))
	if delta > 5*time.Minute || delta < -30*time.Second {
		return false
	}
	sig, e := hex.DecodeString(signature)
	if e != nil {
		return false
	}
	message := make([]byte, 0, len(timestamp)+len(raw))
	message = append(message, timestamp...)
	message = append(message, raw...)
	return ed25519.Verify(key, message, sig)
}
func permissions(raw string, required ...int) bool {
	if len(raw) == 0 || len(raw) > 128 {
		return false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return false
		}
	}
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return false
	}
	if n.Bit(3) == 1 {
		return true
	} // ADMINISTRATOR.
	for _, bit := range required {
		if n.Bit(bit) == 0 {
			return false
		}
	}
	return true
}
func (i Interaction) BindingTarget() (string, integration.Target, bool) {
	target := integration.Target{InteractionID: i.ID, GuildID: i.GuildID, ChannelID: i.ChannelID, ChannelName: i.Channel.Name, DiscordUserID: i.Member.User.ID}
	if i.Type != 2 || !Snowflake(i.ID) || !Snowflake(i.GuildID) || !Snowflake(i.ChannelID) || !Snowflake(i.Member.User.ID) {
		return "", target, false
	}
	if i.Owners["0"] != i.GuildID || i.Channel.Type == nil || *i.Channel.Type != 0 || i.Channel.ID != i.ChannelID {
		return "", target, false
	}
	if !utf8.ValidString(i.Channel.Name) || len(i.Channel.Name) == 0 || utf8.RuneCountInString(i.Channel.Name) > 100 {
		return "", target, false
	}
	// The member must manage AND view this channel; the app must view AND send.
	if !permissions(i.Member.Permissions, 4, 10) || !permissions(i.AppPermissions, 10, 11) {
		return "", target, false
	}
	if i.Data.Name != "taskflow" || i.Data.Type != 1 || len(i.Data.Options) != 1 {
		return "", target, false
	}
	sub := i.Data.Options[0]
	if sub.Name != "bind" || sub.Type != 1 || len(sub.Options) != 1 {
		return "", target, false
	}
	option := sub.Options[0]
	if option.Name != "code" || option.Type != 3 || len(option.Options) != 0 || !integration.ValidCode(option.Value) {
		return "", target, false
	}
	return option.Value, target, true
}
