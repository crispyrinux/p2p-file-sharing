package identity_test

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"p2p-file-sharing/pkg/identity"
)

func TestGenerateIdentity(t *testing.T) {
	id, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity failed: %v", err)
	}

	if len(id.PublicKey) != 32 {
		t.Errorf("expected 32-byte public key, got %d", len(id.PublicKey))
	}
	if len(id.PrivateKey) != 64 {
		t.Errorf("expected 64-byte private key, got %d", len(id.PrivateKey))
	}
	if len(id.PeerID) != 64 {
		t.Errorf("expected 64-character hex PeerID, got %d", len(id.PeerID))
	}

	decoded, err := hex.DecodeString(id.PeerID)
	if err != nil {
		t.Fatalf("PeerID is not valid hex: %v", err)
	}
	if string(decoded) != string(id.PublicKey) {
		t.Errorf("PeerID hex decoding does not match public key bytes")
	}
}

func TestSaveAndLoadIdentity(t *testing.T) {
	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "identity.json")

	orig, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity failed: %v", err)
	}

	if err := orig.SaveToFile(keyPath); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	loaded, err := identity.LoadFromFile(keyPath)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if loaded.PeerID != orig.PeerID {
		t.Errorf("expected PeerID %s, got %s", orig.PeerID, loaded.PeerID)
	}
	if string(loaded.PublicKey) != string(orig.PublicKey) {
		t.Errorf("loaded public key mismatch")
	}
	if string(loaded.PrivateKey) != string(orig.PrivateKey) {
		t.Errorf("loaded private key mismatch")
	}
}

func TestLoadOrInit(t *testing.T) {
	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "identity.json")

	// First call should generate and save
	id1, created, err := identity.LoadOrInit(keyPath)
	if err != nil {
		t.Fatalf("LoadOrInit first call failed: %v", err)
	}
	if !created {
		t.Errorf("expected created to be true on first call")
	}

	// Second call should load existing
	id2, created, err := identity.LoadOrInit(keyPath)
	if err != nil {
		t.Fatalf("LoadOrInit second call failed: %v", err)
	}
	if created {
		t.Errorf("expected created to be false on second call")
	}
	if id2.PeerID != id1.PeerID {
		t.Errorf("expected stable PeerID across LoadOrInit calls: %s != %s", id1.PeerID, id2.PeerID)
	}
}

func TestLoadIdentity_ErrorHandling(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Non-existent file
	_, err := identity.LoadFromFile(filepath.Join(tempDir, "missing.json"))
	if err == nil {
		t.Errorf("expected error loading non-existent file, got nil")
	}

	// 2. Corrupt JSON
	corruptPath := filepath.Join(tempDir, "corrupt.json")
	_ = os.WriteFile(corruptPath, []byte("invalid-json{"), 0600)
	_, err = identity.LoadFromFile(corruptPath)
	if err == nil {
		t.Errorf("expected error loading corrupt JSON, got nil")
	}

	// 3. Short key bytes
	shortKeyPath := filepath.Join(tempDir, "short_key.json")
	_ = os.WriteFile(shortKeyPath, []byte(`{"peer_id":"123","public_key":"AQID","private_key":"BAUG"}`), 0600)
	_, err = identity.LoadFromFile(shortKeyPath)
	if err == nil {
		t.Errorf("expected error with short keys, got nil")
	}

	// 4. PeerID mismatch
	id, _ := identity.GenerateIdentity()
	id.PeerID = "0000000000000000000000000000000000000000000000000000000000000000"
	mismatchPath := filepath.Join(tempDir, "mismatch.json")
	_ = id.SaveToFile(mismatchPath)
	_, err = identity.LoadFromFile(mismatchPath)
	if err == nil {
		t.Errorf("expected error with mismatched peer ID, got nil")
	}

	// 5. LoadOrInit on invalid file
	_, _, err = identity.LoadOrInit(corruptPath)
	if err == nil {
		t.Errorf("expected error in LoadOrInit with corrupt file, got nil")
	}
}
