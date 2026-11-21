package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"p2p-file-sharing/pkg/discovery"
	"p2p-file-sharing/pkg/transfer"
)

func createTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	tempDir := t.TempDir()
	reg := discovery.NewPeerRegistry()

	cfg := ServerConfig{
		Port:       8080,
		P2PPort:    4001,
		PeerID:     "test-peer-id-1234567890abcdef1234567890abcdef1234567890abcdef12345678",
		DataDir:    tempDir,
		MDNSActive: true,
		Registry:   reg,
		StartTime:  time.Now().Add(-10 * time.Minute),
	}

	server, err := NewServer(cfg, DefaultAssets())
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	return server, tempDir
}

func TestServer_StaticAssetServing(t *testing.T) {
	srv, _ := createTestServer(t)

	// Test GET /
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "P2P File Sharing") {
		t.Errorf("expected body to contain 'P2P File Sharing'")
	}

	// Test GET /styles.css
	req = httptest.NewRequest(http.MethodGet, "/styles.css", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for styles.css, got %d", rec.Code)
	}

	// Test GET /app.js
	req = httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for app.js, got %d", rec.Code)
	}
}

func TestServer_NodeStatusEndpoint(t *testing.T) {
	srv, _ := createTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var status NodeStatusView
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to parse status json: %v", err)
	}

	if status.PeerID != "test-peer-id-1234567890abcdef1234567890abcdef1234567890abcdef12345678" {
		t.Errorf("unexpected peer ID: %s", status.PeerID)
	}
	if status.Port != 4001 || status.WebPort != 8080 {
		t.Errorf("unexpected ports: P2P=%d Web=%d", status.Port, status.WebPort)
	}
	if !status.MDNSActive {
		t.Errorf("expected MDNSActive to be true")
	}
	if status.UptimeSeconds < 600 {
		t.Errorf("expected uptime >= 600s, got %d", status.UptimeSeconds)
	}
}

func TestServer_PeerListAndAdd(t *testing.T) {
	srv, _ := createTestServer(t)

	// Initial peers should be empty
	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var peers []PeerView
	if err := json.Unmarshal(rec.Body.Bytes(), &peers); err != nil {
		t.Fatalf("failed to parse peers json: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("expected 0 peers initially, got %d", len(peers))
	}

	// Add manual peer
	body := `{"endpoint": "192.168.1.100:4001"}`
	req = httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	// Re-query peers list
	req = httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	peers = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &peers); err != nil {
		t.Fatalf("failed to parse peers json: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].Endpoint != "192.168.1.100:4001" {
		t.Errorf("expected endpoint 192.168.1.100:4001, got %s", peers[0].Endpoint)
	}
}

func TestServer_FileShares(t *testing.T) {
	srv, tempDir := createTestServer(t)

	// Create a dummy file to share (>= 1 KB)
	testFilePath := filepath.Join(tempDir, "sample.txt")
	if err := os.WriteFile(testFilePath, bytes.Repeat([]byte("A"), 2048), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Publish file via POST /api/share
	reqBody := map[string]string{"file_path": testFilePath}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/share", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var published FileShareView
	if err := json.Unmarshal(rec.Body.Bytes(), &published); err != nil {
		t.Fatalf("failed to parse published share: %v", err)
	}
	if len(published.FileID) != 64 {
		t.Errorf("expected 64-char SHA256 file_id, got %s", published.FileID)
	}
	if published.FileName != "sample.txt" {
		t.Errorf("expected sample.txt, got %s", published.FileName)
	}

	// Query shares via GET /api/shares
	req = httptest.NewRequest(http.MethodGet, "/api/shares", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var shares []FileShareView
	if err := json.Unmarshal(rec.Body.Bytes(), &shares); err != nil {
		t.Fatalf("failed to parse shares list: %v", err)
	}
	if len(shares) != 1 {
		t.Fatalf("expected 1 shared file, got %d", len(shares))
	}
}

func TestServer_SSE_Broadcast(t *testing.T) {
	srv, _ := createTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.Handler().ServeHTTP(rec, req)
	}()

	// Wait briefly for client registration
	time.Sleep(50 * time.Millisecond)

	// Broadcast an event
	srv.EventHub().Broadcast(TelemetryEvent{
		Type:      "chunk_verified",
		FileID:    "abc123456789",
		Index:     3,
		SpeedMbps: 22.4,
	})

	// Allow event to be flushed
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "connected") {
		t.Errorf("expected SSE body to contain 'connected' event, got: %s", body)
	}
	if !strings.Contains(body, "chunk_verified") {
		t.Errorf("expected SSE body to contain 'chunk_verified' event, got: %s", body)
	}
}

func TestServer_ValidationAndErrors(t *testing.T) {
	srv, _ := createTestServer(t)

	// POST /api/peers with empty endpoint
	req := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"endpoint":""}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty peer endpoint, got %d", rec.Code)
	}

	// POST /api/share with nonexistent path
	req = httptest.NewRequest(http.MethodPost, "/api/share", strings.NewReader(`{"file_path":"/nonexistent/file.bin"}`))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for nonexistent share path, got %d", rec.Code)
	}

	// POST /api/download with invalid file_id
	req = httptest.NewRequest(http.MethodPost, "/api/download", strings.NewReader(`{"file_id":"short","peer":"127.0.0.1:4001"}`))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short file_id, got %d", rec.Code)
	}

	// POST /api/download with empty peer
	req = httptest.NewRequest(http.MethodPost, "/api/download", strings.NewReader(`{"file_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","peer":""}`))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty peer, got %d", rec.Code)
	}

	// Static asset 404
	req = httptest.NewRequest(http.MethodGet, "/not-found.html", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing static asset, got %d", rec.Code)
	}
}

func TestServer_TransfersLifecycle(t *testing.T) {
	srv, tempDir := createTestServer(t)

	// Initialize engine with staging
	staging := transfer.NewStagingManager(tempDir)
	engine := transfer.NewTransferEngine(staging)
	srv.cfg.Engine = engine

	// Create test session
	testFID := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	_, err := engine.SessionManager().CreateOrLoadSession(testFID, "test.bin", 1048576, 524288, 2, "peer1", "127.0.0.1:4001", filepath.Join(tempDir, "test.bin"))
	if err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}

	// GET /api/transfers
	req := httptest.NewRequest(http.MethodGet, "/api/transfers", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var transfers []TransferSessionView
	if err := json.Unmarshal(rec.Body.Bytes(), &transfers); err != nil {
		t.Fatalf("failed to parse transfers: %v", err)
	}
	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer session, got %d", len(transfers))
	}

	// POST /api/transfers/{id}/pause
	req = httptest.NewRequest(http.MethodPost, "/api/transfers/"+testFID+"/pause", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// POST /api/transfers/{id}/resume
	req = httptest.NewRequest(http.MethodPost, "/api/transfers/"+testFID+"/resume", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServer_Shutdown(t *testing.T) {
	srv, _ := createTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

