package manifest

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Supported bounds per FR-009.
const (
	MinFileSize uint64 = 1024               // 1 KB
	MaxFileSize uint64 = 2147483648         // 2 GB
	MinChunkSize uint32 = 64 * 1024         // 64 KB
	MaxChunkSize uint32 = 2 * 1024 * 1024   // 2 MB
)

// Validation errors.
var (
	ErrFileSizeOutOfBounds  = errors.New("file size out of bounds (1 KB to 2 GB allowed)")
	ErrChunkSizeOutOfBounds = errors.New("chunk size out of bounds (64 KB to 2 MB allowed)")
	ErrChunkCountMismatch   = errors.New("chunk count does not match total bytes divided by chunk size")
	ErrChunkHashesLength    = errors.New("chunk hashes array length does not match chunk count")
	ErrInvalidFileID        = errors.New("file ID format invalid or mismatch")
	ErrEmptyFileName        = errors.New("file name cannot be empty")
)

// SanitizeFileName strips any path traversal components, slashes, or special characters.
func SanitizeFileName(rawName string) string {
	cleaned := filepath.Base(filepath.Clean(rawName))
	cleaned = strings.ReplaceAll(cleaned, "..", "")
	cleaned = strings.Trim(cleaned, " /\\.")
	if cleaned == "" {
		return "downloaded_file"
	}
	return cleaned
}

// ValidateManifest mathematically verifies all manifest metadata fields before download allocation.
func ValidateManifest(m *FileManifest, expectedFileID string) error {
	if m == nil {
		return errors.New("manifest is nil")
	}

	if m.TotalBytes < MinFileSize || m.TotalBytes > MaxFileSize {
		return fmt.Errorf("%w: %d bytes", ErrFileSizeOutOfBounds, m.TotalBytes)
	}

	if m.ChunkSize < MinChunkSize || m.ChunkSize > MaxChunkSize {
		return fmt.Errorf("%w: %d bytes", ErrChunkSizeOutOfBounds, m.ChunkSize)
	}

	expectedChunkCount := uint32((m.TotalBytes + uint64(m.ChunkSize) - 1) / uint64(m.ChunkSize))
	if m.ChunkCount != expectedChunkCount {
		return fmt.Errorf("%w: declared %d, expected %d", ErrChunkCountMismatch, m.ChunkCount, expectedChunkCount)
	}

	if len(m.ChunkHashes) != int(m.ChunkCount) {
		return fmt.Errorf("%w: len %d != chunk count %d", ErrChunkHashesLength, len(m.ChunkHashes), m.ChunkCount)
	}

	for i, h := range m.ChunkHashes {
		if len(h) != 64 {
			return fmt.Errorf("chunk hash at index %d is not 64 characters", i)
		}
		if _, err := hex.DecodeString(h); err != nil {
			return fmt.Errorf("chunk hash at index %d is not valid hex: %w", i, err)
		}
	}

	if len(m.FileID) != 64 {
		return fmt.Errorf("%w: length %d != 64", ErrInvalidFileID, len(m.FileID))
	}
	if _, err := hex.DecodeString(m.FileID); err != nil {
		return fmt.Errorf("%w: not valid hex: %v", ErrInvalidFileID, err)
	}

	if expectedFileID != "" && m.FileID != expectedFileID {
		return fmt.Errorf("%w: expected %s, got %s", ErrInvalidFileID, expectedFileID, m.FileID)
	}

	sanitized := SanitizeFileName(m.FileName)
	if sanitized == "" {
		return ErrEmptyFileName
	}

	return nil
}
