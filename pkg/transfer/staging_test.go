package transfer_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"p2p-file-sharing/pkg/manifest"
	"p2p-file-sharing/pkg/transfer"
)

func TestStagingAndAssembly(t *testing.T) {
	tempDir := t.TempDir()
	sm := transfer.NewStagingManager(tempDir)

	// Create test file data
	chunk1 := []byte("first chunk 512KB payload content...")
	chunk2 := []byte("second chunk 512KB payload content...")
	wholeData := append(chunk1, chunk2...)

	h1 := sha256.Sum256(chunk1)
	h2 := sha256.Sum256(chunk2)
	hWhole := sha256.Sum256(wholeData)

	fileID := hex.EncodeToString(hWhole[:])

	mf := &manifest.FileManifest{
		FileID:      fileID,
		FileName:    "assembled.txt",
		TotalBytes:  uint64(len(wholeData)),
		ChunkSize:   uint32(len(chunk1)),
		ChunkCount:  2,
		ChunkHashes: []string{hex.EncodeToString(h1[:]), hex.EncodeToString(h2[:])},
	}

	// Save chunk 0
	if err := sm.SaveChunk(fileID, 0, chunk1, mf.ChunkHashes[0]); err != nil {
		t.Fatalf("SaveChunk 0 failed: %v", err)
	}

	// Saving corrupted chunk must fail
	corruptChunk := append([]byte{}, chunk2...)
	corruptChunk[0] ^= 0xFF
	if err := sm.SaveChunk(fileID, 1, corruptChunk, mf.ChunkHashes[1]); err == nil {
		t.Errorf("expected corrupt chunk save to fail, got nil")
	}

	// Attempting assembly before chunk 1 is saved must fail
	targetFile := filepath.Join(tempDir, "downloads", "assembled.txt")
	if err := sm.AssembleFile(mf, targetFile); err == nil {
		t.Errorf("expected assembly to fail with missing chunk 1, got nil")
	}

	// Save valid chunk 1
	if err := sm.SaveChunk(fileID, 1, chunk2, mf.ChunkHashes[1]); err != nil {
		t.Fatalf("SaveChunk 1 failed: %v", err)
	}

	// Assemble file
	if err := sm.AssembleFile(mf, targetFile); err != nil {
		t.Fatalf("AssembleFile failed: %v", err)
	}

	// Verify assembled file content on disk
	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read assembled file: %v", err)
	}
	if string(content) != string(wholeData) {
		t.Errorf("assembled content mismatch")
	}

	// Test CleanStaging
	if err := sm.CleanStaging(fileID); err != nil {
		t.Fatalf("CleanStaging failed: %v", err)
	}
	if sm.HasChunk(fileID, 0) {
		t.Errorf("expected chunk 0 to be cleaned from staging")
	}

	// Assemble with mismatched file ID must fail
	mfMismatched := *mf
	mfMismatched.FileID = "0000000000000000000000000000000000000000000000000000000000000000"
	_ = sm.SaveChunk(mfMismatched.FileID, 0, chunk1, mf.ChunkHashes[0])
	_ = sm.SaveChunk(mfMismatched.FileID, 1, chunk2, mf.ChunkHashes[1])
	if err := sm.AssembleFile(&mfMismatched, filepath.Join(tempDir, "mismatch.txt")); err == nil {
		t.Errorf("expected assembly to fail on whole-file SHA-256 mismatch, got nil")
	}
}
