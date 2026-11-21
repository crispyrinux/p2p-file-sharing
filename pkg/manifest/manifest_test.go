package manifest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"p2p-file-sharing/pkg/manifest"
)

func TestCreateManifestFromReader(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "sample.bin")

	// Create 1.2 MB test file
	fileSize := 1200 * 1024
	data := make([]byte, fileSize)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	expectedWholeHash := sha256.Sum256(data)
	expectedFileID := hex.EncodeToString(expectedWholeHash[:])

	chunkSize := uint32(512 * 1024) // 512 KB
	mf, err := manifest.CreateManifest(filePath, chunkSize)
	if err != nil {
		t.Fatalf("CreateManifest failed: %v", err)
	}

	if mf.FileID != expectedFileID {
		t.Errorf("FileID mismatch: expected %s, got %s", expectedFileID, mf.FileID)
	}
	if mf.TotalBytes != uint64(fileSize) {
		t.Errorf("TotalBytes mismatch: expected %d, got %d", fileSize, mf.TotalBytes)
	}
	if mf.ChunkSize != chunkSize {
		t.Errorf("ChunkSize mismatch: expected %d, got %d", chunkSize, mf.ChunkSize)
	}

	// 1200 KB / 512 KB = 3 chunks (512 KB, 512 KB, 176 KB)
	expectedChunkCount := uint32(3)
	if mf.ChunkCount != expectedChunkCount {
		t.Errorf("ChunkCount mismatch: expected %d, got %d", expectedChunkCount, mf.ChunkCount)
	}
	if len(mf.ChunkHashes) != int(expectedChunkCount) {
		t.Fatalf("ChunkHashes count mismatch: expected %d, got %d", expectedChunkCount, len(mf.ChunkHashes))
	}

	// Verify each chunk hash
	offset := 0
	for i := 0; i < int(expectedChunkCount); i++ {
		end := offset + int(chunkSize)
		if end > fileSize {
			end = fileSize
		}
		chunkSlice := data[offset:end]
		h := sha256.Sum256(chunkSlice)
		expectedChunkHash := hex.EncodeToString(h[:])
		if mf.ChunkHashes[i] != expectedChunkHash {
			t.Errorf("chunk %d hash mismatch: expected %s, got %s", i, expectedChunkHash, mf.ChunkHashes[i])
		}
		offset = end
	}

	// Default chunk size fallback
	mfDefault, err := manifest.CreateManifest(filePath, 0)
	if err != nil || mfDefault.ChunkSize != manifest.DefaultChunkSize {
		t.Errorf("expected default chunk size %d, got %d (err: %v)", manifest.DefaultChunkSize, mfDefault.ChunkSize, err)
	}

	// Non-existent file
	_, err = manifest.CreateManifest(filepath.Join(tempDir, "missing.bin"), 0)
	if err == nil {
		t.Errorf("expected error creating manifest for non-existent file, got nil")
	}

	// Zero-byte file
	zeroPath := filepath.Join(tempDir, "empty.bin")
	_ = os.WriteFile(zeroPath, []byte{}, 0644)
	_, err = manifest.CreateManifest(zeroPath, 0)
	if err == nil {
		t.Errorf("expected error creating manifest for zero-byte file, got nil")
	}
}
