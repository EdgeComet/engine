package cache

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/klauspost/compress/snappy"
	"github.com/pierrec/lz4/v4"

	"github.com/edgecomet/engine/pkg/types"
)

// ErrDecompression is returned when cache decompression fails.
// Use errors.Is(err, ErrDecompression) to check for decompression errors.
var ErrDecompression = errors.New("decompression failed")

// Compress compresses content using the specified algorithm.
// Returns compressed bytes, file extension, and error.
// If content is below threshold or algorithm is "none", returns original with empty extension.
func Compress(content []byte, algorithm string) ([]byte, string, error) {
	// Skip compression for small content
	if len(content) < types.CompressionMinSize {
		return content, "", nil
	}

	// Skip if no compression requested
	if algorithm == types.CompressionNone || algorithm == "" {
		return content, "", nil
	}

	switch algorithm {
	case types.CompressionSnappy:
		compressed := snappy.Encode(nil, content)
		return compressed, types.ExtSnappy, nil

	case types.CompressionLZ4:
		// Use LZ4 stream format which embeds size information
		var buf bytes.Buffer
		w := lz4.NewWriter(&buf)
		if _, err := w.Write(content); err != nil {
			w.Close()
			return nil, "", fmt.Errorf("lz4 compression failed: %w", err)
		}
		if err := w.Close(); err != nil {
			return nil, "", fmt.Errorf("lz4 compression close failed: %w", err)
		}
		return buf.Bytes(), types.ExtLZ4, nil

	default:
		// Unknown algorithm - treat as no compression
		return content, "", nil
	}
}

// Decompress decompresses content based on file path extension.
// Returns original content if not compressed or extension not recognized.
func Decompress(content []byte, filePath string) ([]byte, error) {
	algorithm := DetectAlgorithmFromPath(filePath)

	switch algorithm {
	case types.CompressionSnappy:
		decompressed, err := snappy.Decode(nil, content)
		if err != nil {
			return nil, fmt.Errorf("snappy decompression failed: %w", err)
		}
		return decompressed, nil

	case types.CompressionLZ4:
		// Use LZ4 stream format reader
		r := lz4.NewReader(bytes.NewReader(content))
		decompressed, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("lz4 decompression failed: %w", err)
		}
		return decompressed, nil

	default:
		// Not compressed or unknown format - return as-is
		return content, nil
	}
}

// GetCompressionExt returns the file extension for an algorithm.
func GetCompressionExt(algorithm string) string {
	switch algorithm {
	case types.CompressionSnappy:
		return types.ExtSnappy
	case types.CompressionLZ4:
		return types.ExtLZ4
	default:
		return ""
	}
}

// DetectAlgorithmFromPath returns the compression algorithm from file path.
func DetectAlgorithmFromPath(filePath string) string {
	if strings.HasSuffix(filePath, types.ExtSnappy) {
		return types.CompressionSnappy
	}
	if strings.HasSuffix(filePath, types.ExtLZ4) {
		return types.CompressionLZ4
	}
	return types.CompressionNone
}

// IsCompressed returns true if the file path indicates compression.
func IsCompressed(filePath string) bool {
	return strings.HasSuffix(filePath, types.ExtSnappy) ||
		strings.HasSuffix(filePath, types.ExtLZ4)
}
