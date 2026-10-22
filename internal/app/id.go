package app

import (
	"crypto/rand"
	"encoding/hex"

	"dicegirl/internal/apperr"
)

// newCharacterID generates a program-safe ID "c-" + 8 random hex chars.
// IDs are never derived from display names.
func newCharacterID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", apperr.Wrap("app.newCharacterID", apperr.Internal,
			"cannot generate character id", err)
	}
	return "c-" + hex.EncodeToString(b), nil
}
