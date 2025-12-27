package secret

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateWebhookSecret generates a cryptographically secure random secret.
// Returns a 32-byte hex-encoded string (64 characters).
func GenerateWebhookSecret() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
