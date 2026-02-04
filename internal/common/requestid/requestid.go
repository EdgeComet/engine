package requestid

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const (
	// MaxRequestIDLength is the maximum total length (same as UUID: 36 chars)
	MaxRequestIDLength = 36
	// PrefixLength is the length of the random prefix
	PrefixLength = 5
	// MaxCustomIDLength is the max length for the sanitized custom portion
	// 36 total - 5 prefix - 1 hyphen = 30
	MaxCustomIDLength = MaxRequestIDLength - PrefixLength - 1
)

var (
	// sanitizeRegex removes all characters except a-z, A-Z, 0-9, and hyphens
	sanitizeRegex = regexp.MustCompile(`[^a-zA-Z0-9-]+`)
	// consecutiveHyphensRegex matches one or more consecutive hyphens
	consecutiveHyphensRegex = regexp.MustCompile(`-+`)
)

// GenerateRequestID creates a unique request ID from an optional custom ID.
// If customID is provided, it sanitizes it (keeping only [a-zA-Z0-9-]) and prepends
// 5 random alphanumeric characters for uniqueness.
// Format: {5-random-chars}-{sanitized-custom-id}
// If customID is empty or becomes empty after sanitization, falls back to UUID.
// The total length is capped at 36 characters (UUID length).
func GenerateRequestID(customID string) string {
	// Sanitize custom ID - replace spaces with hyphens, remove other invalid chars
	sanitized := strings.ReplaceAll(customID, " ", "-")
	sanitized = sanitizeRegex.ReplaceAllString(sanitized, "")

	// Replace any consecutive hyphens with a single hyphen
	sanitized = consecutiveHyphensRegex.ReplaceAllString(sanitized, "-")

	sanitized = strings.TrimPrefix(sanitized, "-")
	sanitized = strings.TrimSuffix(sanitized, "-")

	// If no valid custom ID, fallback to UUID
	if sanitized == "" {
		return uuid.New().String()
	}

	// Generate random prefix (5 alphanumeric characters)
	prefix := generateRandomPrefix()

	// Truncate sanitized ID if needed to fit within max length
	if len(sanitized) > MaxCustomIDLength {
		sanitized = sanitized[:MaxCustomIDLength]
	}

	// Combine prefix and sanitized custom ID
	return prefix + "-" + sanitized
}

// generateRandomPrefix creates a 5-character random alphanumeric string using crypto/rand
func generateRandomPrefix() string {
	// Generate enough random bytes for 5 hex characters
	// We need at least 3 bytes to get 6 hex chars, then take first 5
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to UUID-based prefix if crypto/rand fails
		return uuid.New().String()[:PrefixLength]
	}

	// Convert first 8 bytes to hex (16 chars), take first 5
	// Using 8 bytes gives us high entropy for uniqueness
	return hex.EncodeToString(bytes)[:PrefixLength]
}
