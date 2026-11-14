// Package interfaceshttpdiscord decodes Discord's signed protocol; binding state lives in integration.
package interfaceshttpdiscord

import (
	"math/big"
	"unicode/utf8"

	applicationintegration "github.com/pluto-han/taskflow/internal/application/integration"
	domainintegration "github.com/pluto-han/taskflow/internal/domain/integration"
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
	}
	// ADMINISTRATOR.
	for _, bit := range required {
		if n.Bit(bit) == 0 {
			return false
		}
	}

	return true
}

func (i Interaction) BindingTarget() (string, domainintegration.Target, bool) {
	target := domainintegration.Target{
		InteractionID: i.ID,
		GuildID:       i.GuildID,
		ChannelID:     i.ChannelID,
		ChannelName:   i.Channel.Name,
		DiscordUserID: i.Member.User.ID,
	}
	if i.Type != 2 || !domainintegration.Snowflake(i.ID) || !domainintegration.Snowflake(i.GuildID) || !domainintegration.Snowflake(i.ChannelID) || !domainintegration.Snowflake(i.Member.User.ID) {
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
	if option.Name != "code" || option.Type != 3 || len(option.Options) != 0 || !applicationintegration.ValidCode(option.Value) {
		return "", target, false
	}

	return option.Value, target, true
}
