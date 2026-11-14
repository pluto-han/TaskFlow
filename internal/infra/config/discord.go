package infraconfig

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"

	domainintegration "github.com/pluto-han/taskflow/internal/domain/integration"
)

func (c Config) Discord() (ed25519.PublicKey, string, error) {
	if c.DiscordApplicationID == "" && c.DiscordPublicKey == "" {
		return nil, "", nil
	}

	key, e := hex.DecodeString(c.DiscordPublicKey)
	if e != nil || len(key) != ed25519.PublicKeySize || !domainintegration.Snowflake(c.DiscordApplicationID) {
		return nil, "", errors.New("set valid DISCORD_APPLICATION_ID and DISCORD_PUBLIC_KEY together")
	}

	return ed25519.PublicKey(key), c.DiscordApplicationID, nil
}
