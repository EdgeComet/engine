package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/pkg/types"
)

func setupTestDir(t *testing.T) string {
	dir, err := os.MkdirTemp("", "cache-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func newTestFilesystemCache() *FilesystemCache {
	logger := zap.NewNop()
	return NewFilesystemCache(logger)
}

func TestFilesystemCache_CompressWriteReadDecompress_Snappy(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	original := generateTestContent(2000) // Above threshold
	filePath := filepath.Join(dir, "test.html.snappy")

	// Compress and write (production pattern)
	compressed, ext, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)
	assert.Equal(t, types.ExtSnappy, ext)

	err = fc.WriteHTML(filePath, compressed)
	require.NoError(t, err)

	diskSize := int64(len(compressed))
	assert.Greater(t, diskSize, int64(0))
	assert.Less(t, diskSize, int64(len(original)), "compressed should be smaller")

	// Verify file exists
	assert.True(t, fc.FileExists(filePath))

	// Read and decompress
	decompressed, err := fc.ReadCompressed(filePath)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

func TestFilesystemCache_CompressWriteReadDecompress_LZ4(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	original := generateTestContent(2000)
	filePath := filepath.Join(dir, "test.html.lz4")

	// Compress and write (production pattern)
	compressed, ext, err := Compress(original, types.CompressionLZ4)
	require.NoError(t, err)
	assert.Equal(t, types.ExtLZ4, ext)

	err = fc.WriteHTML(filePath, compressed)
	require.NoError(t, err)

	diskSize := int64(len(compressed))
	assert.Greater(t, diskSize, int64(0))
	assert.Less(t, diskSize, int64(len(original)))

	// Read and decompress
	decompressed, err := fc.ReadCompressed(filePath)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

func TestFilesystemCache_CompressWriteReadDecompress_None(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	original := generateTestContent(2000)
	filePath := filepath.Join(dir, "test.html")

	// Compress (returns original for "none") and write (production pattern)
	compressed, ext, err := Compress(original, types.CompressionNone)
	require.NoError(t, err)
	assert.Equal(t, "", ext)

	err = fc.WriteHTML(filePath, compressed)
	require.NoError(t, err)

	diskSize := int64(len(compressed))
	assert.Equal(t, int64(len(original)), diskSize, "no compression should have same size")

	// Read (no decompression needed)
	decompressed, err := fc.ReadCompressed(filePath)
	require.NoError(t, err)
	assert.Equal(t, original, decompressed)
}

func TestFilesystemCache_Compress_SmallContent(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	// Content below threshold - Compress() skips compression
	original := []byte("small content")

	// Compress returns original and empty extension for small content
	compressed, ext, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)
	assert.Equal(t, "", ext, "small content should have no extension")
	assert.Equal(t, original, compressed, "small content should not be compressed")

	// Write without compression extension (production pattern)
	filePath := filepath.Join(dir, "small.html")
	err = fc.WriteHTML(filePath, compressed)
	require.NoError(t, err)

	diskSize := int64(len(compressed))
	assert.Equal(t, int64(len(original)), diskSize, "small content should not be compressed")

	// Read back (no decompression needed for .html)
	content, err := fc.ReadCompressed(filePath)
	require.NoError(t, err)
	assert.Equal(t, original, content)
}

func TestFilesystemCache_ReadCompressed_UncompressedHTML(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	// Write uncompressed .html file directly
	original := []byte("<html><body>Hello World</body></html>")
	filePath := filepath.Join(dir, "page.html")

	err := fc.WriteHTML(filePath, original)
	require.NoError(t, err)

	// ReadCompressed should return content as-is for .html files
	content, err := fc.ReadCompressed(filePath)
	require.NoError(t, err)
	assert.Equal(t, original, content)
}

func TestFilesystemCache_ReadCompressed_DecompressionFailure(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	// Write garbage bytes to .snappy file
	garbage := []byte("this is not valid snappy data")
	filePath := filepath.Join(dir, "corrupt.html.snappy")

	err := fc.WriteHTML(filePath, garbage)
	require.NoError(t, err)

	// ReadCompressed should return error
	_, err = fc.ReadCompressed(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decompression failed")
}

func TestFilesystemCache_WriteHTML_CreatesDirectoryStructure(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	original := generateTestContent(2000)
	filePath := filepath.Join(dir, "deep", "nested", "path", "file.html.snappy")

	// Directory doesn't exist yet
	assert.False(t, fc.FileExists(filepath.Dir(filePath)))

	// Compress and write (production pattern)
	compressed, _, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)

	err = fc.WriteHTML(filePath, compressed)
	require.NoError(t, err)

	// File should exist
	assert.True(t, fc.FileExists(filePath))
}

func TestFilesystemCache_WriteHTML_AtomicWrite(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	original := generateTestContent(2000)
	filePath := filepath.Join(dir, "atomic.html.snappy")
	tempPath := filePath + ".tmp"

	// Compress and write (production pattern)
	compressed, _, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)

	err = fc.WriteHTML(filePath, compressed)
	require.NoError(t, err)

	// Temp file should be cleaned up
	assert.False(t, fc.FileExists(tempPath), "temp file should be removed after successful write")

	// Final file should exist
	assert.True(t, fc.FileExists(filePath))
}

func TestFilesystemCache_ReadCompressed_FileNotFound(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	filePath := filepath.Join(dir, "nonexistent.html.snappy")

	_, err := fc.ReadCompressed(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file not found")
}

func TestFilesystemCache_CompressWrite_VerifyDiskSize(t *testing.T) {
	dir := setupTestDir(t)
	fc := newTestFilesystemCache()

	original := generateTestContent(5000)
	filePath := filepath.Join(dir, "verify.html.snappy")

	// Compress and write (production pattern)
	compressed, _, err := Compress(original, types.CompressionSnappy)
	require.NoError(t, err)

	err = fc.WriteHTML(filePath, compressed)
	require.NoError(t, err)

	diskSize := int64(len(compressed))

	// Verify actual file size matches compressed size
	info, err := os.Stat(filePath)
	require.NoError(t, err)
	assert.Equal(t, diskSize, info.Size(), "compressed size should match actual file size")
}
