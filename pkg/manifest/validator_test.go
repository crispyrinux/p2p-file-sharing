package manifest_test

import (
	"encoding/json"
	"os"
	"testing"

	"p2p-file-sharing/pkg/manifest"
)

type Fixtures struct {
	ValidManifest           manifest.FileManifest `json:"valid_manifest"`
	ZeroByteManifest        manifest.FileManifest `json:"zero_byte_manifest"`
	Sub1KBManifest          manifest.FileManifest `json:"sub_1kb_manifest"`
	OversizedManifest       manifest.FileManifest `json:"oversized_manifest"`
	MismatchedCountManifest manifest.FileManifest `json:"mismatched_chunk_count_manifest"`
	TraversalManifest       manifest.FileManifest `json:"traversal_filename_manifest"`
}

func loadFixtures(t *testing.T) Fixtures {
	data, err := os.ReadFile("../../tests/testdata/manifest_fixtures.json")
	if err != nil {
		t.Fatalf("failed to read test fixtures: %v", err)
	}
	var f Fixtures
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("failed to unmarshal test fixtures: %v", err)
	}
	return f
}

func TestValidateManifest(t *testing.T) {
	fixtures := loadFixtures(t)

	// Valid manifest must pass
	if err := manifest.ValidateManifest(&fixtures.ValidManifest, fixtures.ValidManifest.FileID); err != nil {
		t.Errorf("expected valid manifest to pass, got: %v", err)
	}

	// Nil manifest
	if err := manifest.ValidateManifest(nil, ""); err == nil {
		t.Errorf("expected nil manifest error, got nil")
	}

	// Zero-byte manifest must be rejected
	if err := manifest.ValidateManifest(&fixtures.ZeroByteManifest, fixtures.ZeroByteManifest.FileID); err == nil {
		t.Errorf("expected zero-byte manifest to be rejected, got nil")
	}

	// Sub-1KB manifest must be rejected
	if err := manifest.ValidateManifest(&fixtures.Sub1KBManifest, fixtures.Sub1KBManifest.FileID); err == nil {
		t.Errorf("expected sub-1KB manifest to be rejected, got nil")
	}

	// Oversized manifest (>2GB) must be rejected
	if err := manifest.ValidateManifest(&fixtures.OversizedManifest, fixtures.OversizedManifest.FileID); err == nil {
		t.Errorf("expected oversized manifest to be rejected, got nil")
	}

	// Mismatched chunk count must be rejected
	if err := manifest.ValidateManifest(&fixtures.MismatchedCountManifest, fixtures.MismatchedCountManifest.FileID); err == nil {
		t.Errorf("expected mismatched chunk count manifest to be rejected, got nil")
	}

	// File ID mismatch must be rejected
	if err := manifest.ValidateManifest(&fixtures.ValidManifest, "wrong_file_id"); err == nil {
		t.Errorf("expected mismatched FileID to be rejected, got nil")
	}

	// Path traversal filename must be sanitized
	sanitizedName := manifest.SanitizeFileName(fixtures.TraversalManifest.FileName)
	if sanitizedName != "shadow" {
		t.Errorf("expected sanitized filename 'shadow', got %q", sanitizedName)
	}

	// Empty filename sanitization fallback
	emptySanitized := manifest.SanitizeFileName("   ... /\\ ")
	if emptySanitized != "downloaded_file" {
		t.Errorf("expected fallback 'downloaded_file', got %q", emptySanitized)
	}

	// Chunk size out of bounds (< 64KB or > 2MB)
	mBadChunkSize := fixtures.ValidManifest
	mBadChunkSize.ChunkSize = 1024
	if err := manifest.ValidateManifest(&mBadChunkSize, ""); err == nil {
		t.Errorf("expected error for chunk size < 64KB, got nil")
	}

	mBadChunkSize.ChunkSize = 4 * 1024 * 1024
	if err := manifest.ValidateManifest(&mBadChunkSize, ""); err == nil {
		t.Errorf("expected error for chunk size > 2MB, got nil")
	}

	// Bad chunk hash length or hex
	mBadHash := fixtures.ValidManifest
	mBadHash.ChunkHashes = []string{"short"}
	mBadHash.ChunkCount = 1
	mBadHash.TotalBytes = uint64(mBadHash.ChunkSize)
	if err := manifest.ValidateManifest(&mBadHash, ""); err == nil {
		t.Errorf("expected error for non-64-character chunk hash, got nil")
	}

	mBadHash.ChunkHashes = []string{"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"}
	if err := manifest.ValidateManifest(&mBadHash, ""); err == nil {
		t.Errorf("expected error for non-hex chunk hash, got nil")
	}

	// Bad FileID length or hex
	mBadFID := fixtures.ValidManifest
	mBadFID.FileID = "short"
	if err := manifest.ValidateManifest(&mBadFID, ""); err == nil {
		t.Errorf("expected error for short FileID, got nil")
	}

	mBadFID.FileID = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	if err := manifest.ValidateManifest(&mBadFID, ""); err == nil {
		t.Errorf("expected error for non-hex FileID, got nil")
	}
}
