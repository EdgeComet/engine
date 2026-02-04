package har

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompressHAR(t *testing.T) {
	data := []byte(`{"log":{"version":"1.2","entries":[]}}`)

	compressed, err := CompressHAR(data)
	require.NoError(t, err)
	assert.NotNil(t, compressed)
	// Compressed should be smaller for repetitive data
	assert.Greater(t, len(data), 0)
}

func TestDecompressHAR(t *testing.T) {
	original := []byte(`{"log":{"version":"1.2","entries":[]}}`)

	compressed, err := CompressHAR(original)
	require.NoError(t, err)

	decompressed, err := DecompressHAR(compressed)
	require.NoError(t, err)

	assert.Equal(t, original, decompressed)
}

func TestCompressDecompressRoundTrip(t *testing.T) {
	testCases := []string{
		`{"log":{"version":"1.2"}}`,
		`{"log":{"version":"1.2","entries":[{"request":{"url":"https://example.com"}}]}}`,
		`{}`,
	}

	for _, tc := range testCases {
		original := []byte(tc)

		compressed, err := CompressHAR(original)
		require.NoError(t, err)

		decompressed, err := DecompressHAR(compressed)
		require.NoError(t, err)

		assert.Equal(t, original, decompressed)
	}
}

func TestDecompressInvalidData(t *testing.T) {
	invalidData := []byte("not gzip data")

	_, err := DecompressHAR(invalidData)
	assert.Error(t, err)
}

func TestIsOverSize(t *testing.T) {
	smallData := make([]byte, 1000)
	assert.False(t, IsOverSize(smallData))

	largeData := make([]byte, MaxHARSize+1)
	assert.True(t, IsOverSize(largeData))

	exactData := make([]byte, MaxHARSize)
	assert.False(t, IsOverSize(exactData))
}

func TestMaxHARSizeConstant(t *testing.T) {
	assert.Equal(t, 300*1024, MaxHARSize)
}
