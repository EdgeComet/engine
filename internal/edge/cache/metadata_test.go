package cache

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheMetadata_ToHash(t *testing.T) {
	now := time.Now().Truncate(time.Second) // Truncate to second precision for Unix timestamp

	tests := []struct {
		name     string
		metadata *CacheMetadata
		validate func(t *testing.T, hash map[string]interface{})
	}{
		{
			name: "all fields populated with headers",
			metadata: &CacheMetadata{
				Key:        "cache:1:1:abc123",
				URL:        "https://example.com/test",
				FilePath:   "/cache/1/2025/10/18/file.html",
				HostID:     1,
				Dimension:  "desktop",
				RequestID:  "req-123",
				CreatedAt:  now,
				ExpiresAt:  now.Add(10 * time.Minute),
				Size:       1024,
				LastAccess: now,
				Source:     SourceRender,
				StatusCode: 200,
				Headers: map[string][]string{
					"Content-Type":  {"text/html"},
					"Cache-Control": {"max-age=600"},
				},
			},
			validate: func(t *testing.T, hash map[string]interface{}) {
				assert.Equal(t, "cache:1:1:abc123", hash["key"])
				assert.Equal(t, "https://example.com/test", hash["url"])
				assert.Equal(t, "/cache/1/2025/10/18/file.html", hash["file_path"])
				assert.Equal(t, 1, hash["host_id"])
				assert.Equal(t, "desktop", hash["dimension"])
				assert.Equal(t, "req-123", hash["request_id"])
				assert.Equal(t, now.Unix(), hash["created_at"])
				assert.Equal(t, now.Add(10*time.Minute).Unix(), hash["expires_at"])
				assert.Equal(t, int64(1024), hash["size"])
				assert.Equal(t, now.Unix(), hash["last_access"])
				assert.Equal(t, "render", hash["source"])
				assert.Equal(t, 200, hash["status_code"])
				assert.Contains(t, hash, "headers")
				assert.Contains(t, hash["headers"], "Content-Type")
				assert.Contains(t, hash["headers"], "Cache-Control")
			},
		},
		{
			name: "nil headers",
			metadata: &CacheMetadata{
				Key:        "cache:1:1:xyz789",
				URL:        "https://example.com/page",
				FilePath:   "/cache/file.html",
				HostID:     2,
				Dimension:  "mobile",
				RequestID:  "req-456",
				CreatedAt:  now,
				ExpiresAt:  now.Add(5 * time.Minute),
				Size:       512,
				LastAccess: now,
				Source:     SourceBypass,
				StatusCode: 404,
				Headers:    nil,
			},
			validate: func(t *testing.T, hash map[string]interface{}) {
				assert.Equal(t, "cache:1:1:xyz789", hash["key"])
				assert.Equal(t, 2, hash["host_id"])
				assert.Equal(t, "bypass", hash["source"])
				assert.Equal(t, 404, hash["status_code"])
				assert.NotContains(t, hash, "headers")
			},
		},
		{
			name: "empty headers map",
			metadata: &CacheMetadata{
				Key:        "cache:3:2:def456",
				URL:        "https://example.com/empty",
				FilePath:   "/cache/empty.html",
				HostID:     3,
				Dimension:  "tablet",
				RequestID:  "req-789",
				CreatedAt:  now,
				ExpiresAt:  now.Add(15 * time.Minute),
				Size:       2048,
				LastAccess: now,
				Source:     SourceRender,
				StatusCode: 200,
				Headers:    map[string][]string{},
			},
			validate: func(t *testing.T, hash map[string]interface{}) {
				assert.Equal(t, "cache:3:2:def456", hash["key"])
				assert.NotContains(t, hash, "headers")
			},
		},
		{
			name: "zero timestamps",
			metadata: &CacheMetadata{
				Key:        "cache:1:1:test",
				URL:        "https://example.com",
				FilePath:   "/cache/test.html",
				HostID:     1,
				Dimension:  "desktop",
				RequestID:  "req-000",
				CreatedAt:  time.Unix(0, 0),
				ExpiresAt:  time.Unix(0, 0),
				Size:       100,
				LastAccess: time.Unix(0, 0),
				Source:     SourceRender,
				StatusCode: 200,
			},
			validate: func(t *testing.T, hash map[string]interface{}) {
				assert.Equal(t, int64(0), hash["created_at"])
				assert.Equal(t, int64(0), hash["expires_at"])
				assert.Equal(t, int64(0), hash["last_access"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := tt.metadata.ToHash()

			require.NotNil(t, hash)
			tt.validate(t, hash)
		})
	}
}

func TestCacheMetadata_FromHash(t *testing.T) {
	tests := []struct {
		name     string
		hashData map[string]string
		wantErr  bool
		errField string
		validate func(t *testing.T, metadata *CacheMetadata)
	}{
		{
			name: "valid hash with all fields",
			hashData: map[string]string{
				"key":         "cache:1:1:abc123",
				"url":         "https://example.com/test",
				"file_path":   "/cache/file.html",
				"host_id":     "1",
				"dimension":   "desktop",
				"request_id":  "req-123",
				"created_at":  "1729252800",
				"expires_at":  "1729253400",
				"size":        "1024",
				"last_access": "1729252800",
				"source":      "render",
				"status_code": "200",
				"headers":     `{"Content-Type":["text/html"],"Cache-Control":["max-age=600"]}`,
			},
			wantErr: false,
			validate: func(t *testing.T, metadata *CacheMetadata) {
				assert.Equal(t, "cache:1:1:abc123", metadata.Key)
				assert.Equal(t, "https://example.com/test", metadata.URL)
				assert.Equal(t, "/cache/file.html", metadata.FilePath)
				assert.Equal(t, 1, metadata.HostID)
				assert.Equal(t, "desktop", metadata.Dimension)
				assert.Equal(t, "req-123", metadata.RequestID)
				assert.Equal(t, int64(1729252800), metadata.CreatedAt.Unix())
				assert.Equal(t, int64(1729253400), metadata.ExpiresAt.Unix())
				assert.Equal(t, int64(1024), metadata.Size)
				assert.Equal(t, int64(1729252800), metadata.LastAccess.Unix())
				assert.Equal(t, "render", metadata.Source)
				assert.Equal(t, 200, metadata.StatusCode)
				require.NotNil(t, metadata.Headers)
				assert.Equal(t, []string{"text/html"}, metadata.Headers["Content-Type"])
				assert.Equal(t, []string{"max-age=600"}, metadata.Headers["Cache-Control"])
			},
		},
		{
			name: "valid hash without headers",
			hashData: map[string]string{
				"key":         "cache:2:1:xyz789",
				"url":         "https://example.com/page",
				"file_path":   "/cache/page.html",
				"host_id":     "2",
				"dimension":   "mobile",
				"request_id":  "req-456",
				"created_at":  "1729252800",
				"expires_at":  "1729253400",
				"size":        "512",
				"last_access": "1729252800",
				"source":      "bypass",
				"status_code": "404",
			},
			wantErr: false,
			validate: func(t *testing.T, metadata *CacheMetadata) {
				assert.Equal(t, "cache:2:1:xyz789", metadata.Key)
				assert.Equal(t, 2, metadata.HostID)
				assert.Equal(t, "bypass", metadata.Source)
				assert.Equal(t, 404, metadata.StatusCode)
				assert.Nil(t, metadata.Headers)
			},
		},
		{
			name: "invalid host_id",
			hashData: map[string]string{
				"key":         "cache:1:1:test",
				"url":         "https://example.com",
				"file_path":   "/cache/test.html",
				"host_id":     "invalid",
				"dimension":   "desktop",
				"request_id":  "req-000",
				"created_at":  "1729252800",
				"expires_at":  "1729253400",
				"size":        "100",
				"last_access": "1729252800",
				"source":      "render",
				"status_code": "200",
			},
			wantErr:  true,
			errField: "host_id",
		},
		{
			name: "invalid status_code",
			hashData: map[string]string{
				"key":         "cache:1:1:test",
				"url":         "https://example.com",
				"file_path":   "/cache/test.html",
				"host_id":     "1",
				"dimension":   "desktop",
				"request_id":  "req-000",
				"created_at":  "1729252800",
				"expires_at":  "1729253400",
				"size":        "100",
				"last_access": "1729252800",
				"source":      "render",
				"status_code": "not-a-number",
			},
			wantErr:  true,
			errField: "status_code",
		},
		{
			name: "invalid size",
			hashData: map[string]string{
				"key":         "cache:1:1:test",
				"url":         "https://example.com",
				"file_path":   "/cache/test.html",
				"host_id":     "1",
				"dimension":   "desktop",
				"request_id":  "req-000",
				"created_at":  "1729252800",
				"expires_at":  "1729253400",
				"size":        "invalid",
				"last_access": "1729252800",
				"source":      "render",
				"status_code": "200",
			},
			wantErr:  true,
			errField: "size",
		},
		{
			name: "invalid created_at",
			hashData: map[string]string{
				"key":         "cache:1:1:test",
				"url":         "https://example.com",
				"file_path":   "/cache/test.html",
				"host_id":     "1",
				"dimension":   "desktop",
				"request_id":  "req-000",
				"created_at":  "not-a-timestamp",
				"expires_at":  "1729253400",
				"size":        "100",
				"last_access": "1729252800",
				"source":      "render",
				"status_code": "200",
			},
			wantErr:  true,
			errField: "created_at",
		},
		{
			name: "invalid expires_at",
			hashData: map[string]string{
				"key":         "cache:1:1:test",
				"url":         "https://example.com",
				"file_path":   "/cache/test.html",
				"host_id":     "1",
				"dimension":   "desktop",
				"request_id":  "req-000",
				"created_at":  "1729252800",
				"expires_at":  "invalid",
				"size":        "100",
				"last_access": "1729252800",
				"source":      "render",
				"status_code": "200",
			},
			wantErr:  true,
			errField: "expires_at",
		},
		{
			name: "invalid last_access",
			hashData: map[string]string{
				"key":         "cache:1:1:test",
				"url":         "https://example.com",
				"file_path":   "/cache/test.html",
				"host_id":     "1",
				"dimension":   "desktop",
				"request_id":  "req-000",
				"created_at":  "1729252800",
				"expires_at":  "1729253400",
				"size":        "100",
				"last_access": "invalid",
				"source":      "render",
				"status_code": "200",
			},
			wantErr:  true,
			errField: "last_access",
		},
		{
			name: "invalid headers JSON",
			hashData: map[string]string{
				"key":         "cache:1:1:test",
				"url":         "https://example.com",
				"file_path":   "/cache/test.html",
				"host_id":     "1",
				"dimension":   "desktop",
				"request_id":  "req-000",
				"created_at":  "1729252800",
				"expires_at":  "1729253400",
				"size":        "100",
				"last_access": "1729252800",
				"source":      "bypass",
				"status_code": "200",
				"headers":     "not-valid-json",
			},
			wantErr:  true,
			errField: "headers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := &CacheMetadata{}
			err := metadata.FromHash(tt.hashData)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errField)
			} else {
				require.NoError(t, err)
				tt.validate(t, metadata)
			}
		})
	}
}

func TestCacheMetadata_RoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	tests := []struct {
		name     string
		metadata *CacheMetadata
	}{
		{
			name: "complete metadata with headers",
			metadata: &CacheMetadata{
				Key:        "cache:1:1:abc123",
				URL:        "https://example.com/test?param=value",
				FilePath:   "/cache/1/2025/10/18/07/00/abc123_1.html",
				HostID:     1,
				Dimension:  "desktop",
				RequestID:  "req-12345",
				CreatedAt:  now,
				ExpiresAt:  now.Add(30 * time.Minute),
				Size:       4096,
				LastAccess: now,
				Source:     SourceBypass,
				StatusCode: 200,
				Headers: map[string][]string{
					"Content-Type":  {"text/html; charset=utf-8"},
					"Cache-Control": {"public, max-age=1800"},
					"ETag":          {`"abc123-def456"`},
					"Last-Modified": {"Fri, 18 Oct 2025 07:00:00 GMT"},
				},
			},
		},
		{
			name: "minimal metadata without headers",
			metadata: &CacheMetadata{
				Key:        "cache:2:2:xyz789",
				URL:        "https://example.com/page",
				FilePath:   "/cache/2/2025/10/18/xyz789_2.html",
				HostID:     2,
				Dimension:  "mobile",
				RequestID:  "req-67890",
				CreatedAt:  now,
				ExpiresAt:  now.Add(10 * time.Minute),
				Size:       1024,
				LastAccess: now,
				Source:     SourceRender,
				StatusCode: 404,
				Headers:    nil,
			},
		},
		{
			name: "special characters in URL",
			metadata: &CacheMetadata{
				Key:        "cache:3:1:special",
				URL:        "https://example.com/path?q=hello+world&filter=a%20b%20c",
				FilePath:   "/cache/special.html",
				HostID:     3,
				Dimension:  "desktop",
				RequestID:  "req-special",
				CreatedAt:  now,
				ExpiresAt:  now.Add(5 * time.Minute),
				Size:       512,
				LastAccess: now,
				Source:     SourceRender,
				StatusCode: 200,
			},
		},
		{
			name: "large numbers",
			metadata: &CacheMetadata{
				Key:        "cache:999999:99:largehash",
				URL:        "https://example.com/large",
				FilePath:   "/cache/large.html",
				HostID:     999999,
				Dimension:  "desktop",
				RequestID:  "req-large",
				CreatedAt:  now,
				ExpiresAt:  now.Add(24 * time.Hour),
				Size:       9223372036854775807, // max int64
				LastAccess: now,
				Source:     SourceBypass,
				StatusCode: 200,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Serialize to hash
			hash := tt.metadata.ToHash()
			require.NotNil(t, hash)

			// Convert interface{} values to strings for FromHash
			stringHash := make(map[string]string)
			for k, v := range hash {
				stringHash[k] = interfaceToString(t, v)
			}

			// Deserialize from hash
			result := &CacheMetadata{}
			err := result.FromHash(stringHash)
			require.NoError(t, err)

			// Verify all fields match
			assert.Equal(t, tt.metadata.Key, result.Key)
			assert.Equal(t, tt.metadata.URL, result.URL)
			assert.Equal(t, tt.metadata.FilePath, result.FilePath)
			assert.Equal(t, tt.metadata.HostID, result.HostID)
			assert.Equal(t, tt.metadata.Dimension, result.Dimension)
			assert.Equal(t, tt.metadata.RequestID, result.RequestID)
			assert.Equal(t, tt.metadata.CreatedAt.Unix(), result.CreatedAt.Unix())
			assert.Equal(t, tt.metadata.ExpiresAt.Unix(), result.ExpiresAt.Unix())
			assert.Equal(t, tt.metadata.Size, result.Size)
			assert.Equal(t, tt.metadata.LastAccess.Unix(), result.LastAccess.Unix())
			assert.Equal(t, tt.metadata.Source, result.Source)
			assert.Equal(t, tt.metadata.StatusCode, result.StatusCode)

			// Verify headers
			if tt.metadata.Headers == nil {
				assert.Nil(t, result.Headers)
			} else {
				require.NotNil(t, result.Headers)
				assert.Equal(t, len(tt.metadata.Headers), len(result.Headers))
				for k, v := range tt.metadata.Headers {
					assert.Equal(t, v, result.Headers[k], "Header %s mismatch", k)
				}
			}
		})
	}
}

func TestCacheMetadata_EdgeCases(t *testing.T) {
	t.Run("zero timestamps", func(t *testing.T) {
		metadata := &CacheMetadata{
			Key:        "cache:1:1:zero",
			URL:        "https://example.com",
			FilePath:   "/cache/zero.html",
			HostID:     1,
			Dimension:  "desktop",
			RequestID:  "req-zero",
			CreatedAt:  time.Unix(0, 0),
			ExpiresAt:  time.Unix(0, 0),
			Size:       100,
			LastAccess: time.Unix(0, 0),
			Source:     SourceRender,
			StatusCode: 200,
		}

		hash := metadata.ToHash()
		assert.Equal(t, int64(0), hash["created_at"])
		assert.Equal(t, int64(0), hash["expires_at"])
		assert.Equal(t, int64(0), hash["last_access"])
	})

	t.Run("headers with special characters", func(t *testing.T) {
		metadata := &CacheMetadata{
			Key:        "cache:1:1:special",
			URL:        "https://example.com",
			FilePath:   "/cache/special.html",
			HostID:     1,
			Dimension:  "desktop",
			RequestID:  "req-special",
			CreatedAt:  time.Now(),
			ExpiresAt:  time.Now().Add(10 * time.Minute),
			Size:       100,
			LastAccess: time.Now(),
			Source:     SourceBypass,
			StatusCode: 200,
			Headers: map[string][]string{
				"Content-Type":   {"application/json; charset=utf-8"},
				"X-Custom-Quote": {`Value with "quotes"`},
				"X-Custom-Slash": {"path/to/resource"},
				"X-Unicode":      {"Hello 世界 🌍"},
			},
		}

		hash := metadata.ToHash()
		require.Contains(t, hash, "headers")

		// Convert to string hash for FromHash
		stringHash := make(map[string]string)
		for k, v := range hash {
			stringHash[k] = interfaceToString(t, v)
		}

		result := &CacheMetadata{}
		err := result.FromHash(stringHash)
		require.NoError(t, err)

		assert.Equal(t, []string{`Value with "quotes"`}, result.Headers["X-Custom-Quote"])
		assert.Equal(t, []string{"path/to/resource"}, result.Headers["X-Custom-Slash"])
		assert.Equal(t, []string{"Hello 世界 🌍"}, result.Headers["X-Unicode"])
	})

	t.Run("empty string fields", func(t *testing.T) {
		hashData := map[string]string{
			"key":         "", // Empty key
			"url":         "",
			"file_path":   "",
			"host_id":     "1",
			"dimension":   "",
			"request_id":  "",
			"created_at":  "1729252800",
			"expires_at":  "1729253400",
			"size":        "100",
			"last_access": "1729252800",
			"source":      "",
			"status_code": "200",
		}

		metadata := &CacheMetadata{}
		err := metadata.FromHash(hashData)
		require.NoError(t, err)

		// Empty strings should be preserved
		assert.Equal(t, "", metadata.Key)
		assert.Equal(t, "", metadata.URL)
		assert.Equal(t, "", metadata.FilePath)
		assert.Equal(t, "", metadata.Dimension)
		assert.Equal(t, "", metadata.RequestID)
		assert.Equal(t, "", metadata.Source)
	})
}

// Helper function to convert interface{} to string for round-trip testing
func interfaceToString(t *testing.T, v interface{}) string {
	t.Helper()
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		t.Fatalf("unexpected type %T for value %v", v, v)
		return ""
	}
}

// TestCacheMetadata_LastBotHit tests LastBotHit field handling
func TestCacheMetadata_LastBotHit(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	t.Run("ToHash includes last_bot_hit when set", func(t *testing.T) {
		timestamp := now.Unix()
		metadata := &CacheMetadata{
			Key:        "cache:1:1:abc123",
			URL:        "https://example.com",
			FilePath:   "/cache/file.html",
			HostID:     1,
			Dimension:  "desktop",
			RequestID:  "req-123",
			CreatedAt:  now,
			ExpiresAt:  now.Add(time.Hour),
			Size:       1024,
			LastAccess: now,
			Source:     SourceRender,
			StatusCode: 200,
			LastBotHit: &timestamp,
		}

		hash := metadata.ToHash()

		assert.Equal(t, timestamp, hash["last_bot_hit"])
	})

	t.Run("ToHash omits last_bot_hit when nil", func(t *testing.T) {
		metadata := &CacheMetadata{
			Key:        "cache:1:1:abc123",
			URL:        "https://example.com",
			FilePath:   "/cache/file.html",
			HostID:     1,
			Dimension:  "desktop",
			RequestID:  "req-123",
			CreatedAt:  now,
			ExpiresAt:  now.Add(time.Hour),
			Size:       1024,
			LastAccess: now,
			Source:     SourceRender,
			StatusCode: 200,
			LastBotHit: nil,
		}

		hash := metadata.ToHash()

		_, exists := hash["last_bot_hit"]
		assert.False(t, exists)
	})

	t.Run("FromHash parses last_bot_hit correctly", func(t *testing.T) {
		timestamp := now.Unix()
		data := map[string]string{
			"key":          "cache:1:1:abc123",
			"url":          "https://example.com",
			"file_path":    "/cache/file.html",
			"host_id":      "1",
			"dimension":    "desktop",
			"request_id":   "req-123",
			"created_at":   strconv.FormatInt(now.Unix(), 10),
			"expires_at":   strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			"size":         "1024",
			"last_access":  strconv.FormatInt(now.Unix(), 10),
			"source":       SourceRender,
			"status_code":  "200",
			"last_bot_hit": strconv.FormatInt(timestamp, 10),
		}

		var metadata CacheMetadata
		err := metadata.FromHash(data)
		require.NoError(t, err)

		require.NotNil(t, metadata.LastBotHit)
		assert.Equal(t, timestamp, *metadata.LastBotHit)
	})

	t.Run("FromHash handles missing last_bot_hit field", func(t *testing.T) {
		data := map[string]string{
			"key":         "cache:1:1:abc123",
			"url":         "https://example.com",
			"file_path":   "/cache/file.html",
			"host_id":     "1",
			"dimension":   "desktop",
			"request_id":  "req-123",
			"created_at":  strconv.FormatInt(now.Unix(), 10),
			"expires_at":  strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			"size":        "1024",
			"last_access": strconv.FormatInt(now.Unix(), 10),
			"source":      SourceRender,
			"status_code": "200",
			// last_bot_hit intentionally missing
		}

		var metadata CacheMetadata
		err := metadata.FromHash(data)
		require.NoError(t, err)

		assert.Nil(t, metadata.LastBotHit)
	})

	t.Run("FromHash handles empty last_bot_hit field", func(t *testing.T) {
		data := map[string]string{
			"key":          "cache:1:1:abc123",
			"url":          "https://example.com",
			"file_path":    "/cache/file.html",
			"host_id":      "1",
			"dimension":    "desktop",
			"request_id":   "req-123",
			"created_at":   strconv.FormatInt(now.Unix(), 10),
			"expires_at":   strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			"size":         "1024",
			"last_access":  strconv.FormatInt(now.Unix(), 10),
			"source":       SourceRender,
			"status_code":  "200",
			"last_bot_hit": "", // Empty string
		}

		var metadata CacheMetadata
		err := metadata.FromHash(data)
		require.NoError(t, err)

		assert.Nil(t, metadata.LastBotHit)
	})

	t.Run("Round-trip preserves LastBotHit value", func(t *testing.T) {
		timestamp := now.Unix()
		original := &CacheMetadata{
			Key:        "cache:1:1:abc123",
			URL:        "https://example.com",
			FilePath:   "/cache/file.html",
			HostID:     1,
			Dimension:  "desktop",
			RequestID:  "req-123",
			CreatedAt:  now,
			ExpiresAt:  now.Add(time.Hour),
			Size:       1024,
			LastAccess: now,
			Source:     SourceRender,
			StatusCode: 200,
			LastBotHit: &timestamp,
		}

		// ToHash
		hash := original.ToHash()

		// Convert hash to string map
		stringHash := make(map[string]string)
		for k, v := range hash {
			stringHash[k] = interfaceToString(t, v)
		}

		// FromHash
		var restored CacheMetadata
		err := restored.FromHash(stringHash)
		require.NoError(t, err)

		require.NotNil(t, restored.LastBotHit)
		assert.Equal(t, timestamp, *restored.LastBotHit)
	})

	t.Run("Round-trip preserves nil LastBotHit", func(t *testing.T) {
		original := &CacheMetadata{
			Key:        "cache:1:1:abc123",
			URL:        "https://example.com",
			FilePath:   "/cache/file.html",
			HostID:     1,
			Dimension:  "desktop",
			RequestID:  "req-123",
			CreatedAt:  now,
			ExpiresAt:  now.Add(time.Hour),
			Size:       1024,
			LastAccess: now,
			Source:     SourceRender,
			StatusCode: 200,
			LastBotHit: nil,
		}

		// ToHash
		hash := original.ToHash()

		// Convert hash to string map
		stringHash := make(map[string]string)
		for k, v := range hash {
			stringHash[k] = interfaceToString(t, v)
		}

		// FromHash
		var restored CacheMetadata
		err := restored.FromHash(stringHash)
		require.NoError(t, err)

		assert.Nil(t, restored.LastBotHit)
	})
}

func TestCacheMetadata_IsFresh(t *testing.T) {
	t.Run("fresh cache", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		assert.True(t, meta.IsFresh())
	})

	t.Run("expired cache", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(-1 * time.Hour),
		}
		assert.False(t, meta.IsFresh())
	})

	t.Run("cache expires exactly now", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(-1 * time.Millisecond),
		}
		assert.False(t, meta.IsFresh())
	})
}

func TestCacheMetadata_IsStale(t *testing.T) {
	t.Run("fresh cache is not stale", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		assert.False(t, meta.IsStale(2*time.Hour))
	})

	t.Run("cache in stale period", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(-30 * time.Minute),
		}
		assert.True(t, meta.IsStale(2*time.Hour))
	})

	t.Run("cache expired beyond stale period", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(-3 * time.Hour),
		}
		assert.False(t, meta.IsStale(2*time.Hour))
	})

	t.Run("cache exactly at stale expiration", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(-2 * time.Hour),
		}
		// Just expired the stale period, should not be stale
		assert.False(t, meta.IsStale(2*time.Hour))
	})

	t.Run("cache just entered stale period", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(-1 * time.Second),
		}
		assert.True(t, meta.IsStale(1*time.Hour))
	})

	t.Run("zero stale TTL", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(-1 * time.Second),
		}
		// With zero stale TTL, nothing is stale
		assert.False(t, meta.IsStale(0))
	})
}

func TestCacheMetadata_StaleAge(t *testing.T) {
	t.Run("fresh cache has zero stale age", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(1 * time.Hour),
		}
		assert.Equal(t, time.Duration(0), meta.StaleAge())
	})

	t.Run("expired cache has positive stale age", func(t *testing.T) {
		expiresAt := time.Now().Add(-30 * time.Minute)
		meta := &CacheMetadata{
			ExpiresAt: expiresAt,
		}
		age := meta.StaleAge()
		// Should be approximately 30 minutes (allow some tolerance for execution time)
		assert.Greater(t, age, 29*time.Minute)
		assert.Less(t, age, 31*time.Minute)
	})

	t.Run("cache just expired has near-zero stale age", func(t *testing.T) {
		meta := &CacheMetadata{
			ExpiresAt: time.Now().Add(-10 * time.Millisecond),
		}
		age := meta.StaleAge()
		assert.Greater(t, age, time.Duration(0))
		assert.Less(t, age, 1*time.Second)
	})

	t.Run("very old cache has large stale age", func(t *testing.T) {
		expiresAt := time.Now().Add(-48 * time.Hour)
		meta := &CacheMetadata{
			ExpiresAt: expiresAt,
		}
		age := meta.StaleAge()
		assert.Greater(t, age, 47*time.Hour)
		assert.Less(t, age, 49*time.Hour)
	})
}

func TestCacheMetadata_DiskSize(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	t.Run("ToHash includes disk_size", func(t *testing.T) {
		metadata := &CacheMetadata{
			Key:        "cache:1:1:abc123",
			URL:        "https://example.com/test",
			FilePath:   "/cache/1/2025/01/06/file.html.snappy",
			HostID:     1,
			Dimension:  "desktop",
			RequestID:  "req-123",
			CreatedAt:  now,
			ExpiresAt:  now.Add(time.Hour),
			Size:       5000, // Original size
			DiskSize:   2500, // Compressed to half
			LastAccess: now,
			Source:     SourceRender,
			StatusCode: 200,
		}
		hash := metadata.ToHash()
		assert.Equal(t, int64(5000), hash["size"])
		assert.Equal(t, int64(2500), hash["disk_size"])
	})

	t.Run("FromHash parses disk_size", func(t *testing.T) {
		hash := map[string]string{
			"key":         "cache:1:1:abc123",
			"url":         "https://example.com/test",
			"file_path":   "/cache/1/2025/01/06/file.html.snappy",
			"host_id":     "1",
			"dimension":   "desktop",
			"request_id":  "req-123",
			"created_at":  strconv.FormatInt(now.Unix(), 10),
			"expires_at":  strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			"size":        "5000",
			"disk_size":   "2500",
			"last_access": strconv.FormatInt(now.Unix(), 10),
			"source":      SourceRender,
			"status_code": "200",
		}
		metadata := &CacheMetadata{}
		err := metadata.FromHash(hash)
		require.NoError(t, err)
		assert.Equal(t, int64(5000), metadata.Size)
		assert.Equal(t, int64(2500), metadata.DiskSize)
	})

	t.Run("FromHash backward compat - missing disk_size defaults to 0", func(t *testing.T) {
		hash := map[string]string{
			"key":        "cache:1:1:abc123",
			"url":        "https://example.com/test",
			"file_path":  "/cache/1/2025/01/06/file.html",
			"host_id":    "1",
			"dimension":  "desktop",
			"request_id": "req-123",
			"created_at": strconv.FormatInt(now.Unix(), 10),
			"expires_at": strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			"size":       "5000",
			// disk_size intentionally missing (legacy entry)
			"last_access": strconv.FormatInt(now.Unix(), 10),
			"source":      SourceRender,
			"status_code": "200",
		}
		metadata := &CacheMetadata{}
		err := metadata.FromHash(hash)
		require.NoError(t, err)
		assert.Equal(t, int64(5000), metadata.Size)
		assert.Equal(t, int64(0), metadata.DiskSize) // Defaults to 0
	})

	t.Run("FromHash handles empty disk_size string", func(t *testing.T) {
		hash := map[string]string{
			"key":         "cache:1:1:abc123",
			"url":         "https://example.com/test",
			"file_path":   "/cache/1/2025/01/06/file.html",
			"host_id":     "1",
			"dimension":   "desktop",
			"request_id":  "req-123",
			"created_at":  strconv.FormatInt(now.Unix(), 10),
			"expires_at":  strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			"size":        "5000",
			"disk_size":   "", // Empty string
			"last_access": strconv.FormatInt(now.Unix(), 10),
			"source":      SourceRender,
			"status_code": "200",
		}
		metadata := &CacheMetadata{}
		err := metadata.FromHash(hash)
		require.NoError(t, err)
		assert.Equal(t, int64(0), metadata.DiskSize) // Defaults to 0
	})

	t.Run("FromHash returns error for invalid disk_size", func(t *testing.T) {
		hash := map[string]string{
			"key":         "cache:1:1:abc123",
			"url":         "https://example.com/test",
			"file_path":   "/cache/1/2025/01/06/file.html",
			"host_id":     "1",
			"dimension":   "desktop",
			"request_id":  "req-123",
			"created_at":  strconv.FormatInt(now.Unix(), 10),
			"expires_at":  strconv.FormatInt(now.Add(time.Hour).Unix(), 10),
			"size":        "5000",
			"disk_size":   "not-a-number",
			"last_access": strconv.FormatInt(now.Unix(), 10),
			"source":      SourceRender,
			"status_code": "200",
		}
		metadata := &CacheMetadata{}
		err := metadata.FromHash(hash)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid disk_size")
	})

	t.Run("Round-trip preserves DiskSize", func(t *testing.T) {
		original := &CacheMetadata{
			Key:        "cache:1:1:abc123",
			URL:        "https://example.com/test",
			FilePath:   "/cache/1/2025/01/06/file.html.snappy",
			HostID:     1,
			Dimension:  "desktop",
			RequestID:  "req-123",
			CreatedAt:  now,
			ExpiresAt:  now.Add(time.Hour),
			Size:       10000,
			DiskSize:   3500,
			LastAccess: now,
			Source:     SourceRender,
			StatusCode: 200,
		}

		// Convert to hash
		hash := original.ToHash()

		// Convert hash values to strings (simulating Redis)
		stringHash := make(map[string]string)
		for k, v := range hash {
			switch val := v.(type) {
			case string:
				stringHash[k] = val
			case int:
				stringHash[k] = strconv.Itoa(val)
			case int64:
				stringHash[k] = strconv.FormatInt(val, 10)
			}
		}

		// Parse back
		restored := &CacheMetadata{}
		err := restored.FromHash(stringHash)
		require.NoError(t, err)

		assert.Equal(t, original.Size, restored.Size)
		assert.Equal(t, original.DiskSize, restored.DiskSize)
	})
}

func TestMetadataStore_GetAbsoluteFilePath(t *testing.T) {
	ms := &MetadataStore{cacheDir: "/var/cache/edgecomet"}

	t.Run("valid relative path", func(t *testing.T) {
		path, err := ms.GetAbsoluteFilePath("1/2025/10/18/07/00/abc123_1.html")
		require.NoError(t, err)
		assert.Equal(t, "/var/cache/edgecomet/1/2025/10/18/07/00/abc123_1.html", path)
	})

	t.Run("path traversal escaping cache dir", func(t *testing.T) {
		_, err := ms.GetAbsoluteFilePath("../../etc/cron.d/backdoor")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes cache directory")
	})

	t.Run("path traversal with intermediate components", func(t *testing.T) {
		_, err := ms.GetAbsoluteFilePath("1/2025/../../../../etc/passwd")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes cache directory")
	})

	t.Run("path with dot-dot that stays inside cache dir", func(t *testing.T) {
		path, err := ms.GetAbsoluteFilePath("1/2025/../2025/file.html")
		require.NoError(t, err)
		assert.Equal(t, "/var/cache/edgecomet/1/2025/file.html", path)
	})

	t.Run("empty relative path returns cache dir", func(t *testing.T) {
		path, err := ms.GetAbsoluteFilePath("")
		require.NoError(t, err)
		assert.Equal(t, "/var/cache/edgecomet", path)
	})

	t.Run("single dot-dot escapes", func(t *testing.T) {
		_, err := ms.GetAbsoluteFilePath("..")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes cache directory")
	})

	t.Run("path with compression extension", func(t *testing.T) {
		path, err := ms.GetAbsoluteFilePath("1/2025/10/18/abc123_1.html.snappy")
		require.NoError(t, err)
		assert.Equal(t, "/var/cache/edgecomet/1/2025/10/18/abc123_1.html.snappy", path)
	})
}
