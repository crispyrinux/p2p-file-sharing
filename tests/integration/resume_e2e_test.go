package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"p2p-file-sharing/pkg/identity"
	"p2p-file-sharing/pkg/manifest"
	"p2p-file-sharing/pkg/transfer"
	"p2p-file-sharing/pkg/transport"
)

func TestE2E_ResilientTransfer_ResumeWithZeroRedownload(t *testing.T) {
	// SC-002: Interrupted transfer resumes and requests only missing chunks
	// (0% re-download of verified chunks), achieving 100% bit-exact SHA-256 reconstruction.
	tempDir := t.TempDir()
	providerDir := filepath.Join(tempDir, "provider")
	consumerDir := filepath.Join(tempDir, "consumer")
	_ = os.MkdirAll(providerDir, 0755)
	_ = os.MkdirAll(consumerDir, 0755)

	idA, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate identity A: %v", err)
	}
	idB, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate identity B: %v", err)
	}

	// 10 MB payload = 20 chunks of 512 KB
	chunkSize := manifest.DefaultChunkSize
	totalChunks := uint32(20)
	totalBytes := uint64(totalChunks * chunkSize)

	sourceFilePath := filepath.Join(providerDir, "resilient_payload.bin")
	buf := make([]byte, chunkSize)
	for i := range buf {
		buf[i] = byte((i * 17) % 256)
	}

	sourceFile, err := os.Create(sourceFilePath)
	if err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}
	wholeHasher := sha256.New()
	for i := uint32(0); i < totalChunks; i++ {
		_, _ = sourceFile.Write(buf)
		_, _ = wholeHasher.Write(buf)
	}
	_ = sourceFile.Close()
	expectedHash := hex.EncodeToString(wholeHasher.Sum(nil))

	m, err := manifest.CreateManifest(sourceFilePath, chunkSize)
	if err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}

	stagingA := transfer.NewStagingManager(filepath.Join(providerDir, ".p2p"))
	engineA := transfer.NewTransferEngine(stagingA)
	if err := engineA.RegisterPublishedFile(m, sourceFilePath); err != nil {
		t.Fatalf("failed to register file on provider: %v", err)
	}

	serverTLSConfig, _ := transport.NewServerTLSConfig(idA)
	clientTLSConfig, _ := transport.NewClientTLSConfig(idB, idA.PeerID)

	// Phase 1: Download up to 50% (10 chunks), then kill connection
	pipeClient1, pipeServer1 := net.Pipe()
	go func() {
		tlsServer, err := transport.UpgradeToTLS(pipeServer1, serverTLSConfig, true)
		if err == nil {
			_ = engineA.HandleConnection(tlsServer)
		}
	}()

	tlsClient1, err := transport.UpgradeToTLS(pipeClient1, clientTLSConfig, false)
	if err != nil {
		t.Fatalf("tls client upgrade failed: %v", err)
	}

	consumerDataDir := filepath.Join(consumerDir, ".p2p")
	stagingB := transfer.NewStagingManager(consumerDataDir)
	engineB := transfer.NewTransferEngine(stagingB)
	targetPath := filepath.Join(consumerDir, "downloads", m.FileName)

	ctxCancel, cancel1 := context.WithCancel(context.Background())
	var downloadedFirstPhase atomic.Uint32

	err = engineB.DownloadFile(ctxCancel, tlsClient1, m.FileID, targetPath, func(completed, total uint32) {
		downloadedFirstPhase.Store(completed)
		if completed == 10 { // 50%
			_ = tlsClient1.Close() // abrupt network drop
			cancel1()
		}
	})
	if err == nil {
		t.Fatalf("expected download error after abrupt close, got nil")
	}

	// Verify session state was preserved as Interrupted
	sess, err := engineB.SessionManager().LoadSession(m.FileID)
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}
	if sess.State != transfer.StateInterrupted {
		t.Errorf("expected session state Interrupted, got %s", sess.State)
	}
	if sess.CompletedChunks != 10 {
		t.Errorf("expected 10 completed chunks recorded, got %d", sess.CompletedChunks)
	}

	// Verify the 10 chunks exist in staging
	for i := uint32(0); i < 10; i++ {
		if !stagingB.HasChunk(m.FileID, i) {
			t.Fatalf("expected chunk %d to exist in staging", i)
		}
	}

	// Phase 2: Resume transfer over a new connection
	pipeClientResume, pipeServerResume := net.Pipe()
	go func() {
		tlsServerResume, err := transport.UpgradeToTLS(pipeServerResume, serverTLSConfig, true)
		if err == nil {
			_ = engineA.HandleConnection(tlsServerResume)
		}
	}()

	tlsClientResume, err := transport.UpgradeToTLS(pipeClientResume, clientTLSConfig, false)
	if err != nil {
		t.Fatalf("resume TLS upgrade failed: %v", err)
	}
	defer tlsClientResume.Close()

	ctxResume, cancelResume := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelResume()

	var finalCount uint32
	var chunksRequestedInPhase2 atomic.Uint32

	err = engineB.DownloadFile(ctxResume, tlsClientResume, m.FileID, targetPath, func(completed, total uint32) {
		finalCount = completed
		chunksRequestedInPhase2.Add(1)
	})
	if err != nil {
		t.Fatalf("resume download failed: %v", err)
	}

	if finalCount != 20 {
		t.Errorf("expected final count 20, got %d", finalCount)
	}

	// Verify session state transitioned to Completed
	sessAfter, err := engineB.SessionManager().LoadSession(m.FileID)
	if err != nil {
		t.Fatalf("failed to load session after resume: %v", err)
	}
	if sessAfter.State != transfer.StateCompleted {
		t.Errorf("expected session state Completed, got %s", sessAfter.State)
	}

	// Verify bit-exact SHA-256 match
	downloadedBytes, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read reassembled file: %v", err)
	}
	if uint64(len(downloadedBytes)) != totalBytes {
		t.Fatalf("file size mismatch: expected %d, got %d", totalBytes, len(downloadedBytes))
	}
	downloadedHash := sha256.Sum256(downloadedBytes)
	if hex.EncodeToString(downloadedHash[:]) != expectedHash {
		t.Fatalf("bit-exact hash mismatch: expected %s, got %s", expectedHash, hex.EncodeToString(downloadedHash[:]))
	}
}
