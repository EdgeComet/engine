package cache

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edgecomet/engine/pkg/types"
)

// generateTestContent creates test content of specified size
func generateTestContent(size int) []byte {
	content := make([]byte, size)
	// Fill with repeatable pattern for good compression
	pattern := []byte("The quick brown fox jumps over the lazy dog. ")
	for i := 0; i < size; i++ {
		content[i] = pattern[i%len(pattern)]
	}
	return content
}

// TestCompressDecompressRoundTripSnappy tests snappy round-trip
func TestCompressDecompressRoundTripSnappy(t *testing.T) {
	original := generateTestContent(2000) // Above threshold

	compressed, ext, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)
	assert.Equal(t, types.ExtSnappy, ext)
	assert.True(t, len(compressed) < len(original), "compressed should be smaller than original")

	decompressed, err := Decompress(compressed, "test.html"+ext)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

// TestCompressDecompressRoundTripLZ4 tests lz4 round-trip
func TestCompressDecompressRoundTripLZ4(t *testing.T) {
	original := generateTestContent(2000) // Above threshold

	compressed, ext, err := Compress(original, types.CompressionLZ4)
	require.NoError(t, err)
	assert.Equal(t, types.ExtLZ4, ext)
	assert.True(t, len(compressed) < len(original), "compressed should be smaller than original")

	decompressed, err := Decompress(compressed, "test.html"+ext)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

// TestCompressSkipsBelowThreshold tests that small content is not compressed
func TestCompressSkipsBelowThreshold(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{"empty content", 0},
		{"small content", 100},
		{"just below threshold", types.CompressionMinSize - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := generateTestContent(tt.size)

			compressed, ext, err := Compress(original, types.CompressionSnappy)
			require.NoError(t, err)
			assert.Empty(t, ext, "extension should be empty when not compressed")
			assert.Equal(t, original, compressed, "content should be unchanged")
		})
	}
}

// TestCompressAtThreshold tests content exactly at threshold
func TestCompressAtThreshold(t *testing.T) {
	original := generateTestContent(types.CompressionMinSize)

	compressed, ext, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)
	assert.Equal(t, types.ExtSnappy, ext, "content at threshold should be compressed")
	assert.True(t, len(compressed) < len(original), "compressed should be smaller")
}

// TestCompressSkipsWhenAlgorithmNone tests that "none" algorithm skips compression
func TestCompressSkipsWhenAlgorithmNone(t *testing.T) {
	original := generateTestContent(2000)

	compressed, ext, err := Compress(original, types.CompressionNone)
	require.NoError(t, err)
	assert.Empty(t, ext)
	assert.Equal(t, original, compressed)
}

// TestCompressSkipsWhenAlgorithmEmpty tests that empty algorithm skips compression
func TestCompressSkipsWhenAlgorithmEmpty(t *testing.T) {
	original := generateTestContent(2000)

	compressed, ext, err := Compress(original, "")
	require.NoError(t, err)
	assert.Empty(t, ext)
	assert.Equal(t, original, compressed)
}

// TestCompressUnknownAlgorithm tests that unknown algorithm is treated as none
func TestCompressUnknownAlgorithm(t *testing.T) {
	original := generateTestContent(2000)

	compressed, ext, err := Compress(original, "unknown")
	require.NoError(t, err)
	assert.Empty(t, ext)
	assert.Equal(t, original, compressed)
}

// TestDecompressUncompressedHTML tests that .html files are returned as-is
func TestDecompressUncompressedHTML(t *testing.T) {
	original := []byte("<html><body>Hello World</body></html>")

	result, err := Decompress(original, "test.html")
	require.NoError(t, err)
	assert.Equal(t, original, result)
}

// TestDecompressUnknownExtension tests that unknown extensions are returned as-is
func TestDecompressUnknownExtension(t *testing.T) {
	original := []byte("some content")

	result, err := Decompress(original, "test.xyz")
	require.NoError(t, err)
	assert.Equal(t, original, result)
}

// TestDecompressCorruptedSnappy tests that corrupted snappy data returns error
func TestDecompressCorruptedSnappy(t *testing.T) {
	corrupted := []byte("this is not valid snappy data")

	_, err := Decompress(corrupted, "test.html.snappy")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "snappy decompression failed")
}

// TestDecompressCorruptedLZ4 tests that corrupted lz4 data returns error
func TestDecompressCorruptedLZ4(t *testing.T) {
	corrupted := []byte("this is not valid lz4 data")

	_, err := Decompress(corrupted, "test.html.lz4")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "lz4 decompression failed")
}

// TestCompressLargeContent tests compression of large content (1MB+)
func TestCompressLargeContent(t *testing.T) {
	original := generateTestContent(1024 * 1024) // 1MB

	// Test snappy
	compressedSnappy, ext, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)
	assert.Equal(t, types.ExtSnappy, ext)
	assert.True(t, len(compressedSnappy) < len(original))

	decompressedSnappy, err := Decompress(compressedSnappy, "test.html.snappy")
	require.NoError(t, err)
	assert.Equal(t, original, decompressedSnappy)

	// Test lz4
	compressedLZ4, ext, err := Compress(original, types.CompressionLZ4)
	require.NoError(t, err)
	assert.Equal(t, types.ExtLZ4, ext)
	assert.True(t, len(compressedLZ4) < len(original))

	decompressedLZ4, err := Decompress(compressedLZ4, "test.html.lz4")
	require.NoError(t, err)
	assert.Equal(t, original, decompressedLZ4)
}

// TestCompressBinaryContent tests compression of binary (non-UTF8) content
func TestCompressBinaryContent(t *testing.T) {
	// Create binary content with all byte values
	original := make([]byte, 2000)
	for i := range original {
		original[i] = byte(i % 256)
	}

	compressed, ext, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)
	assert.Equal(t, types.ExtSnappy, ext)

	decompressed, err := Decompress(compressed, "test.html.snappy")
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

// TestGetCompressionExt tests extension mapping
func TestGetCompressionExt(t *testing.T) {
	tests := []struct {
		algorithm string
		expected  string
	}{
		{types.CompressionSnappy, types.ExtSnappy},
		{types.CompressionLZ4, types.ExtLZ4},
		{types.CompressionNone, ""},
		{"", ""},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			result := GetCompressionExt(tt.algorithm)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestDetectAlgorithmFromPath tests algorithm detection from file paths
func TestDetectAlgorithmFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"test.html.snappy", types.CompressionSnappy},
		{"test.html.lz4", types.CompressionLZ4},
		{"test.html", types.CompressionNone},
		{"/path/to/cache/1/2025/01/06/hash_d1.html.snappy", types.CompressionSnappy},
		{"/path/to/cache/1/2025/01/06/hash_d1.html.lz4", types.CompressionLZ4},
		{"/path/to/cache/1/2025/01/06/hash_d1.html", types.CompressionNone},
		{"", types.CompressionNone},
		{".snappy", types.CompressionSnappy},
		{".lz4", types.CompressionLZ4},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := DetectAlgorithmFromPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsCompressed tests compression detection
func TestIsCompressed(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"test.html.snappy", true},
		{"test.html.lz4", true},
		{"test.html", false},
		{"/path/to/file.snappy", true},
		{"/path/to/file.lz4", true},
		{"/path/to/file.html", false},
		{"", false},
		{"snappy", false}, // Not a suffix
		{"lz4", false},    // Not a suffix
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsCompressed(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCompressEmptyContent tests empty content handling
func TestCompressEmptyContent(t *testing.T) {
	original := []byte{}

	compressed, ext, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)
	assert.Empty(t, ext)
	assert.Empty(t, compressed)
}

// TestDecompressEmptyContent tests empty content decompression
func TestDecompressEmptyContent(t *testing.T) {
	original := []byte{}

	result, err := Decompress(original, "test.html")
	require.NoError(t, err)
	assert.Empty(t, result)
}

// TestCompressRealHTMLContent tests with realistic HTML content
func TestCompressRealHTMLContent(t *testing.T) {
	html := strings.Repeat(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Test Page</title>
</head>
<body>
    <h1>Welcome to the Test Page</h1>
    <p>This is a paragraph with some content that should compress well.</p>
</body>
</html>
`, 5) // Repeat to get above threshold

	original := []byte(html)
	require.True(t, len(original) >= types.CompressionMinSize)

	// Test snappy compression
	compressed, ext, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)
	assert.Equal(t, types.ExtSnappy, ext)

	// HTML compresses well
	compressionRatio := float64(len(compressed)) / float64(len(original))
	assert.Less(t, compressionRatio, 0.5, "HTML should compress to less than 50%% of original")

	// Verify round-trip
	decompressed, err := Decompress(compressed, "page.html.snappy")
	require.NoError(t, err)
	assert.True(t, bytes.Equal(original, decompressed))
}
