package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"p2p-file-sharing/pkg/discovery"
	"p2p-file-sharing/pkg/identity"
	"p2p-file-sharing/pkg/manifest"
	"p2p-file-sharing/pkg/transfer"
	"p2p-file-sharing/pkg/transport"
	"p2p-file-sharing/pkg/web"
)

func TestE2E_WebTransferVisualizer(t *testing.T) {
	tempDir := t.TempDir()
	providerDir := filepath.Join(tempDir, "provider")
	consumerDir := filepath.Join(tempDir, "consumer")
	_ = os.MkdirAll(providerDir, 0755)
	_ = os.MkdirAll(consumerDir, 0755)

	// Step 1: Create a 2 MB test file on provider
	testData := make([]byte, 2*1024*1024)
	for i := range testData {
		testData[i] = byte(i % 251)
	}
	expectedHash := sha256.Sum256(testData)
	expectedHexHash := hex.EncodeToString(expectedHash[:])

	providerFile := filepath.Join(providerDir, "dataset.bin")
	if err := os.WriteFile(providerFile, testData, 0644); err != nil {
		t.Fatalf("failed to write provider file: %v", err)
	}

	// Create manifest with 512 KB chunks (4 chunks total)
	m, err := manifest.CreateManifest(providerFile, 512*1024)
	if err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}
	if m.ChunkCount != 4 {
		t.Fatalf("expected 4 chunks, got %d", m.ChunkCount)
	}

	// Setup Provider P2P Daemon
	providerID, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate provider identity: %v", err)
	}
	providerTLS, err := transport.NewServerTLSConfig(providerID)
	if err != nil {
		t.Fatalf("failed to create provider TLS: %v", err)
	}

	providerListener, err := transport.ListenTLS("127.0.0.1:0", providerTLS)
	if err != nil {
		t.Fatalf("failed to bind provider listener: %v", err)
	}
	defer providerListener.Close()
	providerAddr := providerListener.Addr().String()

	providerStaging := transfer.NewStagingManager(providerDir)
	providerEngine := transfer.NewTransferEngine(providerStaging)
	if err := providerEngine.RegisterPublishedFile(m, providerFile); err != nil {
		t.Fatalf("failed to register published file: %v", err)
	}

	go func() {
		for {
			conn, err := providerListener.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = providerEngine.HandleConnection(conn)
			}()
		}
	}()

	// Step 2: Setup Consumer with Web Server
	consumerID, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate consumer identity: %v", err)
	}

	consumerStaging := transfer.NewStagingManager(consumerDir)
	consumerEngine := transfer.NewTransferEngine(consumerStaging)
	consumerRegistry := discovery.NewPeerRegistry()

	webCfg := web.ServerConfig{
		Port:       8080,
		P2PPort:    4002,
		PeerID:     consumerID.PeerID,
		DataDir:    consumerDir,
		MDNSActive: false,
		Registry:   consumerRegistry,
		Engine:     consumerEngine,
		Identity:   consumerID,
		StartTime:  time.Now(),
	}

	webServer, err := web.NewServer(webCfg, web.DefaultAssets())
	if err != nil {
		t.Fatalf("failed to create web server: %v", err)
	}

	ts := httptest.NewServer(webServer.Handler())
	defer ts.Close()

	// Step 3: Register Provider Peer via Web API POST /api/peers
	addPeerReq := map[string]string{"endpoint": providerAddr}
	addPeerJSON, _ := json.Marshal(addPeerReq)
	resp, err := http.Post(ts.URL+"/api/peers", "application/json", strings.NewReader(string(addPeerJSON)))
	if err != nil {
		t.Fatalf("failed to post peer: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for add peer, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Step 4: Subscribe to SSE stream /api/events
	ctx, cancelSSE := context.WithCancel(context.Background())
	defer cancelSSE()

	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	sseResp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("failed to connect to SSE: %v", err)
	}
	defer sseResp.Body.Close()

	var muEvents sync.Mutex
	var receivedChunks []int
	completedTransfer := make(chan struct{})

	go func() {
		scanner := bufio.NewScanner(sseResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				dataStr := strings.TrimPrefix(line, "data: ")
				var ev web.TelemetryEvent
				if err := json.Unmarshal([]byte(dataStr), &ev); err == nil {
					muEvents.Lock()
					if ev.Type == "chunk_verified" {
						receivedChunks = append(receivedChunks, ev.Index)
					} else if ev.Type == "transfer_state" && ev.State == "Completed" {
						select {
						case <-completedTransfer:
						default:
							close(completedTransfer)
						}
					}
					muEvents.Unlock()
				}
			}
		}
	}()

	// Wait for SSE handshake
	time.Sleep(100 * time.Millisecond)

	// Step 5: Start Download via POST /api/download
	downloadReq := web.DownloadRequest{
		FileID:    m.FileID,
		Peer:      providerAddr,
		OutputDir: filepath.Join(consumerDir, "downloads"),
	}
	dlJSON, _ := json.Marshal(downloadReq)
	resp, err = http.Post(ts.URL+"/api/download", "application/json", strings.NewReader(string(dlJSON)))
	if err != nil {
		t.Fatalf("failed to post download: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Step 6: Wait for transfer completion via SSE
	select {
	case <-completedTransfer:
		// Transfer completed!
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for transfer completion event via SSE")
	}

	// Step 7: Verify all 4 chunks received over SSE telemetry
	muEvents.Lock()
	if len(receivedChunks) != 4 {
		t.Errorf("expected 4 chunk_verified events, got %d (%v)", len(receivedChunks), receivedChunks)
	}
	muEvents.Unlock()

	// Step 8: Verify file integrity
	downloadedFile := filepath.Join(consumerDir, "downloads", m.FileID+".bin")
	downloadedData, err := os.ReadFile(downloadedFile)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	actualHash := sha256.Sum256(downloadedData)
	actualHexHash := hex.EncodeToString(actualHash[:])

	if actualHexHash != expectedHexHash {
		t.Fatalf("file hash mismatch: expected %s, got %s", expectedHexHash, actualHexHash)
	}
}
