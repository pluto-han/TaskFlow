package infradiscord

import (
	"crypto/ed25519"
	"encoding/hex"
	"strconv"
	"time"
)

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
