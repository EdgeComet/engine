package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type CacheBuilder struct {
	basePath string
}

func NewCacheBuilder(basePath string) *CacheBuilder {
	return &CacheBuilder{basePath: basePath}
}

// CreateTestDirectory creates cache dir with timestamp in path
func (cb *CacheBuilder) CreateTestDirectory(hostID int, ageMinutes int) string {
	timestamp := time.Now().UTC().Add(-time.Duration(ageMinutes) * time.Minute)
	path := filepath.Join(
		cb.basePath,
		fmt.Sprintf("%d", hostID),
		timestamp.Format("2006"),
		timestamp.Format("01"),
		timestamp.Format("02"),
		timestamp.Format("15"),
		timestamp.Format("04"),
	)

	if err := os.MkdirAll(path, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create test directory: %v", err))
	}

	// Create test files
	os.WriteFile(filepath.Join(path, "test1.html"), []byte("<html>test1</html>"), 0644)
	os.WriteFile(filepath.Join(path, "test2.html"), []byte("<html>test2</html>"), 0644)

	return path
}

// CreateNestedDirectory creates a cache directory with nested subdirectories and files
func (cb *CacheBuilder) CreateNestedDirectory(hostID int, ageMinutes int) string {
	minutePath := cb.CreateTestDirectory(hostID, ageMinutes)

	// Create nested structure: subdir1/subdir2/
	nestedPath := filepath.Join(minutePath, "subdir1", "subdir2")
	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		panic(fmt.Sprintf("Failed to create nested directory: %v", err))
	}

	// Create files at different levels
	os.WriteFile(filepath.Join(minutePath, "root.html"), []byte("root level file"), 0644)
	os.WriteFile(filepath.Join(minutePath, "subdir1", "level1.html"), []byte("level 1 file"), 0644)
	os.WriteFile(filepath.Join(nestedPath, "level2.html"), []byte("level 2 file"), 0644)

	return minutePath
}

// DirectoryExists checks if directory exists
func (cb *CacheBuilder) DirectoryExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Cleanup removes all test cache directories
func (cb *CacheBuilder) Cleanup() error {
	return os.RemoveAll(cb.basePath)
}
