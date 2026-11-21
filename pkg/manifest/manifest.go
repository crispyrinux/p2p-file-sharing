package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// DefaultChunkSize is 512 KB (524,288 bytes).
const DefaultChunkSize uint32 = 512 * 1024

// FileManifest represents the immutable metadata document of a shared file.
type FileManifest struct {
	FileID      string    `json:"file_id"`
	FileName    string    `json:"file_name"`
	TotalBytes  uint64    `json:"total_bytes"`
	ChunkSize   uint32    `json:"chunk_size"`
	ChunkCount  uint32    `json:"chunk_count"`
	ChunkHashes []string  `json:"chunk_hashes"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateManifest scans a file, computes chunk hashes and the whole-file SHA-256 FileID.
func CreateManifest(filePath string, chunkSize uint32) (*FileManifest, error) {
	if chunkSize == 0 {
		chunkSize = DefaultChunkSize
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	totalBytes := uint64(stat.Size())
	if totalBytes == 0 {
		return nil, errors.New("cannot create manifest for zero-byte file")
	}

	wholeHasher := sha256.New()
	chunkBuffer := make([]byte, chunkSize)
	var chunkHashes []string

	for {
		n, readErr := io.ReadFull(file, chunkBuffer)
		if n > 0 {
			chunkData := chunkBuffer[:n]
			wholeHasher.Write(chunkData)

			chunkHash := sha256.Sum256(chunkData)
			chunkHashes = append(chunkHashes, hex.EncodeToString(chunkHash[:]))
		}

		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}

	wholeDigest := wholeHasher.Sum(nil)
	fileID := hex.EncodeToString(wholeDigest)
	baseName := filepath.Base(filePath)

	return &FileManifest{
		FileID:      fileID,
		FileName:    baseName,
		TotalBytes:  totalBytes,
		ChunkSize:   chunkSize,
		ChunkCount:  uint32(len(chunkHashes)),
		ChunkHashes: chunkHashes,
		CreatedAt:   time.Now().UTC(),
	}, nil
}
