package requestid

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateRequestID(t *testing.T) {
	tests := []struct {
		name          string
		customID      string
		expectUUID    bool
		expectPattern string
	}{
		{
			name:          "empty custom ID returns UUID",
			customID:      "",
			expectUUID:    true,
			expectPattern: "",
		},
		{
			name:          "simple alphanumeric custom ID",
			customID:      "my-request",
			expectUUID:    false,
			expectPattern: `^[a-f0-9]{5}-my-request$`,
		},
		{
			name:          "custom ID with special characters",
			customID:      "my@request#123!",
			expectUUID:    false,
			expectPattern: `^[a-f0-9]{5}-myrequest123$`,
		},
		{
			name:          "custom ID with spaces",
			customID:      "my request 123",
			expectUUID:    false,
			expectPattern: `^[a-f0-9]{5}-my-request-123$`,
		},
		{
			name:          "only special characters returns UUID",
			customID:      "@#$%^&*()",
			expectUUID:    true,
			expectPattern: "",
		},
		{
			name:          "leading and trailing hyphens removed",
			customID:      "---my-request---",
			expectUUID:    false,
			expectPattern: `^[a-f0-9]{5}-my-request$`,
		},
		{
			name:       "very long custom ID is truncated",
			customID:   strings.Repeat("a", 100),
			expectUUID: false,
			// 5 char prefix + 1 hyphen + 30 char custom = 36 total
			expectPattern: `^[a-f0-9]{5}-a{30}$`,
		},
		{
			name:          "mixed case preserved",
			customID:      "MyRequest-123",
			expectUUID:    false,
			expectPattern: `^[a-f0-9]{5}-MyRequest-123$`,
		},
		{
			name:          "numbers only",
			customID:      "123456",
			expectUUID:    false,
			expectPattern: `^[a-f0-9]{5}-123456$`,
		},
		{
			name:          "single character",
			customID:      "x",
			expectUUID:    false,
			expectPattern: `^[a-f0-9]{5}-x$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateRequestID(tt.customID)

			// Check max length
			assert.LessOrEqual(t, len(result), MaxRequestIDLength,
				"Request ID should not exceed max length")

			if tt.expectUUID {
				// Should be a valid UUID format
				uuidPattern := regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
				assert.True(t, uuidPattern.MatchString(result),
					"Expected UUID format, got: %s", result)
			} else {
				// Should match the custom pattern
				pattern := regexp.MustCompile(tt.expectPattern)
				assert.True(t, pattern.MatchString(result),
					"Expected pattern %s, got: %s", tt.expectPattern, result)
			}
		})
	}
}

func TestGenerateRequestID_Uniqueness(t *testing.T) {
	// Generate multiple IDs with same custom ID to verify uniqueness
	// Note: 5-hex-char prefix has ~1M possibilities (16^5 = 1,048,576)
	// Using 100 iterations keeps collision probability very low (~0.5%)
	// while still validating uniqueness mechanism
	customID := "test-request"
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id := GenerateRequestID(customID)
		require.False(t, seen[id], "Generated duplicate request ID: %s", id)
		seen[id] = true
	}
}

func TestGenerateRequestID_Format(t *testing.T) {
	customID := "my-test-request"
	result := GenerateRequestID(customID)

	// Verify format: {5-hex}-{custom}
	parts := strings.SplitN(result, "-", 2)
	require.Len(t, parts, 2, "Request ID should have prefix-custom format")

	prefix := parts[0]
	assert.Len(t, prefix, PrefixLength, "Prefix should be exactly 5 characters")
	assert.Regexp(t, `^[a-f0-9]{5}$`, prefix, "Prefix should be lowercase hex")

	custom := parts[1]
	assert.Equal(t, "my-test-request", custom, "Custom part should be preserved")
}

func TestGenerateRequestID_MaxLength(t *testing.T) {
	// Test that even with very long custom IDs, we don't exceed max length
	longCustomID := strings.Repeat("abc", 50) // 150 characters
	result := GenerateRequestID(longCustomID)

	assert.Equal(t, MaxRequestIDLength, len(result),
		"Result should be exactly %d characters", MaxRequestIDLength)

	// Verify it starts with 5 char prefix + hyphen
	assert.Regexp(t, `^[a-f0-9]{5}-`, result, "Should start with hex prefix")
}

func TestGenerateRequestID_Sanitization(t *testing.T) {
	tests := []struct {
		input    string
		expected string // pattern to match after prefix
	}{
		{"hello world", "hello-world"}, // Custom handling needed
		{"test@example", "testexample"},
		{"foo_bar", "foobar"},
		{"123-456", "123-456"}, // hyphens preserved
		{"CamelCase", "CamelCase"},
		{"test--double", "test-double"},  // double hyphens
		{"test---triple", "test-triple"}, // triple hyphens
		{"test----quad", "test-quad"},    // quadruple hyphens
		{"a-----b", "a-b"},               // many consecutive hyphens
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := GenerateRequestID(tt.input)
			// Extract custom part after prefix
			parts := strings.SplitN(result, "-", 2)
			require.Len(t, parts, 2)

			// Note: spaces get removed entirely by our regex
			actualCustom := parts[1]
			expectedCustom := strings.ReplaceAll(tt.expected, " ", "")

			assert.Equal(t, expectedCustom, actualCustom,
				"Sanitization of %q failed", tt.input)
		})
	}
}

func TestGenerateRandomPrefix(t *testing.T) {
	// Test that prefix generation works and produces expected format
	prefix := generateRandomPrefix()

	assert.Len(t, prefix, PrefixLength, "Prefix should be 5 characters")
	assert.Regexp(t, `^[a-f0-9]{5}$`, prefix, "Prefix should be lowercase hex")
}

func TestGenerateRandomPrefix_Uniqueness(t *testing.T) {
	// Generate many prefixes to check for uniqueness
	seen := make(map[string]bool)
	iterations := 10000

	for i := 0; i < iterations; i++ {
		prefix := generateRandomPrefix()
		seen[prefix] = true
	}

	// With 5 hex chars (16^5 = 1,048,576 possibilities),
	// 10k samples should produce mostly unique values
	uniqueCount := len(seen)
	assert.Greater(t, uniqueCount, iterations*95/100,
		"Expected >95%% unique prefixes, got %d/%d", uniqueCount, iterations)
}
