package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"p2p-file-sharing/pkg/manifest"
	"p2p-file-sharing/pkg/protocol"
)

func TestTransferEngine_DirectTransfer(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "provider")
	consumerDir := filepath.Join(tempDir, "consumer")
	_ = os.MkdirAll(sourceDir, 0755)
	_ = os.MkdirAll(consumerDir, 0755)

	// Create test file of 2 MB
	testFileSize := 2 * 1024 * 1024
	testData := make([]byte, testFileSize)
	for i := range testData {
		testData[i] = byte(i % 251)
	}

	sourceFilePath := filepath.Join(sourceDir, "payload.bin")
	if err := os.WriteFile(sourceFilePath, testData, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	m, err := manifest.CreateManifest(sourceFilePath, manifest.DefaultChunkSize)
	if err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}

	providerStaging := NewStagingManager(filepath.Join(sourceDir, ".p2p"))
	providerEngine := NewTransferEngine(providerStaging)
	if err := providerEngine.RegisterPublishedFile(m, sourceFilePath); err != nil {
		t.Fatalf("failed to register file: %v", err)
	}

	consumerStaging := NewStagingManager(filepath.Join(consumerDir, ".p2p"))
	consumerEngine := NewTransferEngine(consumerStaging)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		_ = providerEngine.HandleConnection(serverConn)
	}()

	targetPath := filepath.Join(consumerDir, "downloads", m.FileName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	progressCalls := 0
	progressCb := func(completed, total uint32) {
		progressCalls++
		if completed > total {
			t.Errorf("completed %d exceeded total %d", completed, total)
		}
	}

	err = consumerEngine.DownloadFile(ctx, clientConn, m.FileID, targetPath, progressCb)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	if progressCalls == 0 {
		t.Errorf("expected progress callbacks, got 0")
	}

	// Verify assembled file bit-exact match
	downloadedData, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if len(downloadedData) != testFileSize {
		t.Fatalf("downloaded file size mismatch: expected %d, got %d", testFileSize, len(downloadedData))
	}
	downloadedDigest := sha256.Sum256(downloadedData)
	if hex.EncodeToString(downloadedDigest[:]) != m.FileID {
		t.Fatalf("downloaded file SHA-256 mismatch: expected %s, got %s", m.FileID, hex.EncodeToString(downloadedDigest[:]))
	}
}

func TestTransferEngine_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	consumerDir := filepath.Join(tempDir, "consumer")
	consumerStaging := NewStagingManager(filepath.Join(consumerDir, ".p2p"))
	consumerEngine := NewTransferEngine(consumerStaging)

	providerStaging := NewStagingManager(filepath.Join(tempDir, "provider", ".p2p"))
	providerEngine := NewTransferEngine(providerStaging)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		_ = providerEngine.HandleConnection(serverConn)
	}()

	dummyFileID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	targetPath := filepath.Join(consumerDir, "notfound.bin")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := consumerEngine.DownloadFile(ctx, clientConn, dummyFileID, targetPath, nil)
	if err == nil {
		t.Fatalf("expected error for non-existent file, got nil")
	}
}

func TestTransferEngine_CorruptChunkRejected(t *testing.T) {
	tempDir := t.TempDir()
	sourceDir := filepath.Join(tempDir, "provider")
	consumerDir := filepath.Join(tempDir, "consumer")

	testData := []byte("this is a valid test payload that is above 1024 bytes and will be corrupted in transmission.......................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................")
	sourceFilePath := filepath.Join(sourceDir, "test.txt")
	_ = os.MkdirAll(sourceDir, 0755)
	_ = os.WriteFile(sourceFilePath, testData, 0644)

	m, err := manifest.CreateManifest(sourceFilePath, manifest.DefaultChunkSize)
	if err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}

	consumerStaging := NewStagingManager(filepath.Join(consumerDir, ".p2p"))
	consumerEngine := NewTransferEngine(consumerStaging)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Custom malicious server sending corrupted chunk
	go func() {
		op, _, err := protocol.ReadFrame(serverConn)
		if err != nil {
			return
		}
		if op == protocol.OpManifestRequest {
			manifestBytes, _ := json.Marshal(m)
			resp := &protocol.ManifestResponseMessage{
				Status:   protocol.ManifestStatusSuccess,
				Manifest: manifestBytes,
			}
			_ = protocol.WriteFrame(serverConn, protocol.OpManifestResponse, resp.Encode())
		}

		// Read chunk request
		op, _, err = protocol.ReadFrame(serverConn)
		if err != nil {
			return
		}
		if op == protocol.OpChunkRequest {
			// Send corrupted chunk
			var fid [32]byte
			rawFID, _ := hex.DecodeString(m.FileID)
			copy(fid[:], rawFID)
			corruptData := []byte("corrupted chunk data bytes that do not match expected manifest hash")
			checksum := sha256.Sum256(corruptData)

			resp := &protocol.ChunkResponseMessage{
				FileID:   fid,
				Index:    0,
				Checksum: checksum,
				Data:     corruptData,
			}
			_ = protocol.WriteFrame(serverConn, protocol.OpChunkResponse, resp.Encode())
		}
	}()

	targetPath := filepath.Join(consumerDir, "corrupted.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = consumerEngine.DownloadFile(ctx, clientConn, m.FileID, targetPath, nil)
	if err == nil {
		t.Fatalf("expected download to fail due to corrupted chunk, got nil")
	}
}

func TestTransferEngine_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()
	staging := NewStagingManager(tempDir)
	engine := NewTransferEngine(staging)

	// Register non-existent file
	m := &manifest.FileManifest{
		FileID:     "abc",
		TotalBytes: 100,
	}
	err := engine.RegisterPublishedFile(m, filepath.Join(tempDir, "missing.bin"))
	if err == nil {
		t.Errorf("expected error registering missing file, got nil")
	}

	// Register with file size mismatch
	existingFile := filepath.Join(tempDir, "exist.bin")
	_ = os.WriteFile(existingFile, []byte("short"), 0644)
	err = engine.RegisterPublishedFile(m, existingFile)
	if err == nil {
		t.Errorf("expected error registering file with size mismatch, got nil")
	}

	// DownloadFile with invalid hex FileID
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	err = engine.DownloadFile(context.Background(), clientConn, "not-hex-id", "target", nil)
	if err == nil {
		t.Errorf("expected error downloading with bad hex file ID, got nil")
	}
}

func TestHeartbeat_BackgroundTimeout(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Server does not respond to ping
	hm := NewHeartbeatMonitor(clientConn, 100*time.Millisecond, 200*time.Millisecond)

	timeoutTriggered := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	hm.Start(ctx, func() {
		close(timeoutTriggered)
	})

	select {
	case <-timeoutTriggered:
		// Success: timeout callback invoked
	case <-time.After(1 * time.Second):
		t.Errorf("expected heartbeat timeout callback to be invoked, timed out")
	}
}
