package har

import (
	"bytes"
	"compress/gzip"
	"io"
)

// HAR size constants
const (
	MaxHARSize = 300 * 1024 // 300KB compressed
)

// CompressHAR compresses HAR JSON data using gzip
func CompressHAR(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}

	if _, err := writer.Write(data); err != nil {
		writer.Close()
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// DecompressHAR decompresses gzip HAR data
func DecompressHAR(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// IsOverSize checks if compressed data exceeds the maximum size
func IsOverSize(data []byte) bool {
	return len(data) > MaxHARSize
}
