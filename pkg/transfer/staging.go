package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"p2p-file-sharing/pkg/manifest"
)

// StagingManager handles temporary chunk storage and whole-file atomic assembly.
type StagingManager struct {
	rootDir string
}

// NewStagingManager initializes a staging manager in the provided directory.
func NewStagingManager(rootDir string) *StagingManager {
	return &StagingManager{rootDir: rootDir}
}

func (s *StagingManager) stagingDir(fileID string) string {
	return filepath.Join(s.rootDir, "staging", fileID)
}

func (s *StagingManager) chunkPath(fileID string, index uint32) string {
	return filepath.Join(s.stagingDir(fileID), fmt.Sprintf("%d.chunk", index))
}

// SaveChunk verifies the chunk SHA-256 hash against the expected manifest hash and writes it to staging.
func (s *StagingManager) SaveChunk(fileID string, index uint32, data []byte, expectedHash string) error {
	sum := sha256.Sum256(data)
	actualHash := hex.EncodeToString(sum[:])
	if actualHash != expectedHash {
		return fmt.Errorf("chunk %d hash mismatch: expected %s, got %s", index, expectedHash, actualHash)
	}

	dir := s.stagingDir(fileID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return os.WriteFile(s.chunkPath(fileID, index), data, 0600)
}

// HasChunk checks if a chunk file exists in staging.
func (s *StagingManager) HasChunk(fileID string, index uint32) bool {
	_, err := os.Stat(s.chunkPath(fileID, index))
	return err == nil
}

// ReadChunk reads a chunk payload from staging.
func (s *StagingManager) ReadChunk(fileID string, index uint32) ([]byte, error) {
	return os.ReadFile(s.chunkPath(fileID, index))
}

// AssembleFile reassembles all staged chunks in order, writes to targetPath, and verifies whole-file SHA-256.
func (s *StagingManager) AssembleFile(m *manifest.FileManifest, targetPath string) error {
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	outFile, err := os.Create(targetPath)
	if err != nil {
		return err
	}

	wholeHasher := sha256.New()
	multiWriter := io.MultiWriter(outFile, wholeHasher)

	for i := uint32(0); i < m.ChunkCount; i++ {
		chunkBytes, err := s.ReadChunk(m.FileID, i)
		if err != nil {
			outFile.Close()
			_ = os.Remove(targetPath)
			return fmt.Errorf("missing chunk %d during assembly: %w", i, err)
		}

		if _, err := multiWriter.Write(chunkBytes); err != nil {
			outFile.Close()
			_ = os.Remove(targetPath)
			return fmt.Errorf("failed to write chunk %d to assembled file: %w", i, err)
		}
	}

	if err := outFile.Close(); err != nil {
		_ = os.Remove(targetPath)
		return err
	}

	finalDigest := hex.EncodeToString(wholeHasher.Sum(nil))
	if finalDigest != m.FileID {
		_ = os.Remove(targetPath)
		return fmt.Errorf("post-assembly whole-file SHA-256 mismatch: expected %s, got %s", m.FileID, finalDigest)
	}

	return nil
}

// CleanStaging removes all staged chunks for a given file ID.
func (s *StagingManager) CleanStaging(fileID string) error {
	return os.RemoveAll(s.stagingDir(fileID))
}
