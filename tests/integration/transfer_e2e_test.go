package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"p2p-file-sharing/pkg/identity"
	"p2p-file-sharing/pkg/manifest"
	"p2p-file-sharing/pkg/transfer"
	"p2p-file-sharing/pkg/transport"
)

func TestE2E_50MB_DirectTransfer(t *testing.T) {
	// SC-001: Direct transfer of a 50 MB file completes with 100% bit-exact SHA-256 match
	// between two nodes over in-memory pipe within 30 seconds.
	tempDir := t.TempDir()
	nodeADir := filepath.Join(tempDir, "nodeA")
	nodeBDir := filepath.Join(tempDir, "nodeB")
	_ = os.MkdirAll(nodeADir, 0755)
	_ = os.MkdirAll(nodeBDir, 0755)

	// 1. Generate identities
	idA, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate identity A: %v", err)
	}
	idB, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate identity B: %v", err)
	}

	// 2. Generate 50 MB test file
	fileSize := 50 * 1024 * 1024
	sourceFilePath := filepath.Join(nodeADir, "50mb_payload.bin")
	sourceFile, err := os.Create(sourceFilePath)
	if err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	buf := make([]byte, 1024*1024) // 1MB buffer
	for i := range buf {
		buf[i] = byte((i * 31) % 256)
	}
	wholeHasher := sha256.New()
	for i := 0; i < 50; i++ {
		_, _ = sourceFile.Write(buf)
		_, _ = wholeHasher.Write(buf)
	}
	_ = sourceFile.Close()
	expectedHash := hex.EncodeToString(wholeHasher.Sum(nil))

	// 3. Node A creates manifest and registers file
	m, err := manifest.CreateManifest(sourceFilePath, manifest.DefaultChunkSize)
	if err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}
	if m.FileID != expectedHash {
		t.Fatalf("manifest file ID mismatch: expected %s, got %s", expectedHash, m.FileID)
	}

	stagingA := transfer.NewStagingManager(filepath.Join(nodeADir, ".p2p"))
	engineA := transfer.NewTransferEngine(stagingA)
	if err := engineA.RegisterPublishedFile(m, sourceFilePath); err != nil {
		t.Fatalf("failed to register published file on Node A: %v", err)
	}

	// 4. Set up TLS configs
	serverTLSConfig, err := transport.NewServerTLSConfig(idA)
	if err != nil {
		t.Fatalf("failed to create server TLS config: %v", err)
	}
	clientTLSConfig, err := transport.NewClientTLSConfig(idB, idA.PeerID)
	if err != nil {
		t.Fatalf("failed to create client TLS config: %v", err)
	}

	// 5. Connect via net.Pipe
	rawClient, rawServer := net.Pipe()

	serverTLSChan := make(chan net.Conn, 1)
	serverErrChan := make(chan error, 1)

	go func() {
		tlsServer, err := transport.UpgradeToTLS(rawServer, serverTLSConfig, true)
		if err != nil {
			serverErrChan <- err
			return
		}
		serverTLSChan <- tlsServer
	}()

	tlsClient, err := transport.UpgradeToTLS(rawClient, clientTLSConfig, false)
	if err != nil {
		t.Fatalf("client TLS upgrade failed: %v", err)
	}
	defer tlsClient.Close()

	select {
	case err := <-serverErrChan:
		t.Fatalf("server TLS upgrade failed: %v", err)
	case tlsServer := <-serverTLSChan:
		defer tlsServer.Close()
		go func() {
			_ = engineA.HandleConnection(tlsServer)
		}()
	}

	// 6. Node B downloads file
	stagingB := transfer.NewStagingManager(filepath.Join(nodeBDir, ".p2p"))
	engineB := transfer.NewTransferEngine(stagingB)

	targetPath := filepath.Join(nodeBDir, "downloads", m.FileName)
	startTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var chunksReported uint32
	err = engineB.DownloadFile(ctx, tlsClient, m.FileID, targetPath, func(completed, total uint32) {
		chunksReported = completed
	})
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}

	elapsed := time.Since(startTime)
	if elapsed > 30*time.Second {
		t.Errorf("SC-001 violation: transfer took %v, exceeded 30 seconds limit", elapsed)
	}
	if chunksReported != m.ChunkCount {
		t.Errorf("expected %d chunks reported, got %d", m.ChunkCount, chunksReported)
	}

	// 7. Verify bit-exact reconstruction
	destData, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if len(destData) != fileSize {
		t.Fatalf("downloaded file size mismatch: expected %d, got %d", fileSize, len(destData))
	}

	actualHash := sha256.Sum256(destData)
	if hex.EncodeToString(actualHash[:]) != expectedHash {
		t.Fatalf("bit-exact hash mismatch: expected %s, got %s", expectedHash, hex.EncodeToString(actualHash[:]))
	}
}
