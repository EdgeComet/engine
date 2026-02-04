package cache

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// FilesystemCache handles reading and writing HTML files to the filesystem
type FilesystemCache struct {
	logger *zap.Logger
}

// NewFilesystemCache creates a new FilesystemCache instance
func NewFilesystemCache(logger *zap.Logger) *FilesystemCache {
	return &FilesystemCache{
		logger: logger,
	}
}

// WriteHTML writes HTML content to the filesystem using atomic write pattern
// It creates the directory structure if needed and writes to a temp file before renaming
// Note: Empty content is allowed (e.g., for redirect responses with no body)
func (fc *FilesystemCache) WriteHTML(filePath string, htmlContent []byte) error {
	// Ensure directory structure exists
	if err := fc.ensureDirectory(filePath); err != nil {
		fc.logger.Error("Failed to create directory structure",
			zap.String("file_path", filePath),
			zap.Error(err))
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write to temporary file first (atomic write pattern)
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, htmlContent, 0644); err != nil {
		fc.logger.Error("Failed to write temporary file",
			zap.String("temp_path", tempPath),
			zap.Error(err))
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename from temp to final file
	if err := os.Rename(tempPath, filePath); err != nil {
		// Cleanup temp file on failure
		os.Remove(tempPath)
		fc.logger.Error("Failed to rename temp file to final path",
			zap.String("temp_path", tempPath),
			zap.String("file_path", filePath),
			zap.Error(err))
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	fc.logger.Debug("HTML file written successfully",
		zap.String("file_path", filePath),
		zap.Int("size_bytes", len(htmlContent)))

	return nil
}

// ReadHTML reads HTML content from the filesystem
// This is primarily for debugging/validation as cache serving uses SendFile
func (fc *FilesystemCache) ReadHTML(filePath string) ([]byte, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found %s: %w", filePath, err)
		}
		fc.logger.Error("Failed to read HTML file",
			zap.String("file_path", filePath),
			zap.Error(err))
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	fc.logger.Debug("HTML file read successfully",
		zap.String("file_path", filePath),
		zap.Int("size_bytes", len(content)))

	return content, nil
}

// ReadCompressed reads content from disk and decompresses based on file extension.
// Returns decompressed content or error.
func (fc *FilesystemCache) ReadCompressed(filePath string) ([]byte, error) {
	// Read raw bytes from disk
	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found %s: %w", filePath, err)
		}
		fc.logger.Error("Failed to read file",
			zap.String("file_path", filePath),
			zap.Error(err))
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Decompress based on file extension
	decompressed, err := Decompress(content, filePath)
	if err != nil {
		fc.logger.Error("Failed to decompress content",
			zap.String("file_path", filePath),
			zap.Error(err))
		return nil, fmt.Errorf("decompression failed: %w", err)
	}

	fc.logger.Debug("Compressed file read successfully",
		zap.String("file_path", filePath),
		zap.Int("disk_size", len(content)),
		zap.Int("decompressed_size", len(decompressed)))

	return decompressed, nil
}

// ensureDirectory creates the directory structure for the given file path
// It creates all parent directories with appropriate permissions
func (fc *FilesystemCache) ensureDirectory(filePath string) error {
	dir := filepath.Dir(filePath)

	// Check if directory already exists
	if _, err := os.Stat(dir); err == nil {
		return nil // Directory exists
	}

	// Create directory structure with parent directories
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory structure: %w", err)
	}

	fc.logger.Debug("Created directory structure", zap.String("directory", dir))
	return nil
}

// FileExists checks if a file exists at the given path
func (fc *FilesystemCache) FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// DeleteFile removes a file from the filesystem
func (fc *FilesystemCache) DeleteFile(filePath string) error {
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}
		fc.logger.Error("Failed to delete file",
			zap.String("file_path", filePath),
			zap.Error(err))
		return fmt.Errorf("failed to delete file: %w", err)
	}

	fc.logger.Debug("File deleted successfully", zap.String("file_path", filePath))
	return nil
}
