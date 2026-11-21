package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"p2p-file-sharing/pkg/identity"
	"p2p-file-sharing/pkg/transfer"
	"p2p-file-sharing/pkg/transport"
)

func TestCLI_Init(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Human readable output
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := Run([]string{"init", "--data-dir", tempDir}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("expected exit code %d, got %d. stderr: %s", ExitSuccess, code, stderr.String())
	}
	outStr := stdout.String()
	if !strings.Contains(outStr, "Initialized new node identity") {
		t.Errorf("expected output to contain initialization message, got: %s", outStr)
	}
	if !strings.Contains(outStr, "Peer ID:") {
		t.Errorf("expected output to contain Peer ID, got: %s", outStr)
	}

	// 2. JSON output
	stdout.Reset()
	stderr.Reset()
	tempDir2 := t.TempDir()
	code = Run([]string{"init", "--data-dir", tempDir2, "--json"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("expected exit code %d, got %d", ExitSuccess, code)
	}
	var res map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse json output: %v. Output: %s", err, stdout.String())
	}
	if res["status"] != "success" || res["command"] != "init" {
		t.Errorf("unexpected json response: %v", res)
	}
	data, ok := res["data"].(map[string]interface{})
	if !ok || data["peer_id"] == "" {
		t.Errorf("missing peer_id in data: %v", res)
	}
}

func TestCLI_Share(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, ".p2p")

	// Init identity first
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	_ = Run([]string{"init", "--data-dir", dataDir}, &stdout, &stderr)

	// Create a test file of 10 KB
	testFilePath := filepath.Join(tempDir, "sample.bin")
	testData := make([]byte, 10*1024)
	for i := range testData {
		testData[i] = byte(i % 127)
	}
	_ = os.WriteFile(testFilePath, testData, 0644)

	// Test share with human output
	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"share", testFilePath, "--data-dir", dataDir}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("expected exit code %d, got %d. stderr: %s", ExitSuccess, code, stderr.String())
	}
	outStr := stdout.String()
	if !strings.Contains(outStr, "File published successfully") {
		t.Errorf("expected success message, got: %s", outStr)
	}
	if !strings.Contains(outStr, "File ID:") {
		t.Errorf("expected File ID, got: %s", outStr)
	}

	// Test share with JSON
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"share", testFilePath, "--data-dir", dataDir, "--json"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("expected exit code %d, got %d", ExitSuccess, code)
	}
	var res map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse json output: %v", err)
	}
	if res["status"] != "success" || res["command"] != "share" {
		t.Errorf("unexpected json response: %v", res)
	}

	// Test share non-existent file
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"share", filepath.Join(tempDir, "does-not-exist.bin"), "--data-dir", dataDir}, &stdout, &stderr)
	if code != ExitValidation {
		t.Errorf("expected exit code %d for missing file, got %d", ExitValidation, code)
	}
}

func TestCLI_Download_ValidationErrors(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// Missing args
	code := Run([]string{"download"}, &stdout, &stderr)
	if code != ExitValidation {
		t.Errorf("expected exit code %d for missing args, got %d", ExitValidation, code)
	}

	// Bad file ID
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"download", "--file-id", "invalid-id", "--peer", "127.0.0.1:4001"}, &stdout, &stderr)
	if code != ExitValidation {
		t.Errorf("expected exit code %d for invalid file-id, got %d", ExitValidation, code)
	}
}

func TestCLI_PeersAndStatus(t *testing.T) {
	tempDir := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	// 1. Status empty
	code := Run([]string{"status", "--data-dir", tempDir}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("expected status exit code %d, got %d", ExitSuccess, code)
	}
	if !strings.Contains(stdout.String(), "No active or paused") {
		t.Errorf("unexpected status output: %s", stdout.String())
	}

	// 2. Status JSON
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"status", "--data-dir", tempDir, "--json"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("expected status JSON exit code %d, got %d", ExitSuccess, code)
	}

	// 3. Peers empty
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"peers", "--data-dir", tempDir}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("expected peers exit code %d, got %d", ExitSuccess, code)
	}
	if !strings.Contains(stdout.String(), "No peers discovered") {
		t.Errorf("unexpected peers output: %s", stdout.String())
	}

	// 4. Peers add manual
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"peers", "--data-dir", tempDir, "--add", "192.168.1.100:4001"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("expected peers add exit code %d, got %d", ExitSuccess, code)
	}
	if !strings.Contains(stdout.String(), "192.168.1.100:4001") {
		t.Errorf("expected added peer in output: %s", stdout.String())
	}

	// 5. Unknown command
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"unknown-cmd"}, &stdout, &stderr)
	if code != ExitValidation {
		t.Errorf("expected validation exit code for unknown command, got %d", code)
	}

	// 6. Empty args
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{}, &stdout, &stderr)
	if code != ExitValidation {
		t.Errorf("expected validation exit code for empty args, got %d", code)
	}
}

func TestCLI_FullDownload(t *testing.T) {
	tempDir := t.TempDir()
	providerDir := filepath.Join(tempDir, "provider")
	consumerDir := filepath.Join(tempDir, "consumer")

	// 1. Init provider
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	_ = Run([]string{"init", "--data-dir", providerDir}, &stdout, &stderr)

	// 2. Share file on provider
	testFile := filepath.Join(tempDir, "data.txt")
	testData := []byte("this is a test payload for CLI download that is at least 1024 bytes long...........................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................................")
	_ = os.WriteFile(testFile, testData, 0644)

	stdout.Reset()
	stderr.Reset()
	code := Run([]string{"share", testFile, "--data-dir", providerDir, "--json"}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Fatalf("share failed: %s", stderr.String())
	}
	var shareRes map[string]interface{}
	_ = json.Unmarshal(stdout.Bytes(), &shareRes)
	dataMap := shareRes["data"].(map[string]interface{})
	fileID := dataMap["file_id"].(string)

	// 3. Start provider TLS listener
	idPath := filepath.Join(providerDir, "identity.json")
	id, err := identity.LoadFromFile(idPath)
	if err != nil {
		t.Fatalf("failed to load identity: %v", err)
	}
	tlsConfig, err := transport.NewServerTLSConfig(id)
	if err != nil {
		t.Fatalf("failed to create server TLS config: %v", err)
	}

	listener, err := transport.ListenTLS("127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatalf("ListenTLS failed: %v", err)
	}
	defer listener.Close()

	staging := transfer.NewStagingManager(providerDir)
	engine := transfer.NewTransferEngine(staging)
	loadSharedFiles(providerDir, engine)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = engine.HandleConnection(conn)
			}()
		}
	}()

	// 4. Run CLI download on consumer with --json
	outDir := filepath.Join(consumerDir, "downloads")
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{
		"download",
		"--file-id", fileID,
		"--peer", listener.Addr().String(),
		"--data-dir", consumerDir,
		"--output-dir", outDir,
		"--json",
	}, &stdout, &stderr)

	if code != ExitSuccess {
		t.Fatalf("download failed with code %d. stderr: %s", code, stderr.String())
	}

	var dlRes map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &dlRes); err != nil {
		t.Fatalf("failed to parse download json: %v", err)
	}
	if dlRes["status"] != "success" {
		t.Errorf("expected success status, got: %v", dlRes)
	}

	// 5. Test unreachable peer returns ExitNetwork (code 3)
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{
		"download",
		"--file-id", fileID,
		"--peer", "127.0.0.1:59999", // closed port
		"--data-dir", consumerDir,
		"--output-dir", outDir,
	}, &stdout, &stderr)

	if code != ExitNetwork {
		t.Errorf("expected ExitNetwork (3) for unreachable peer, got %d", code)
	}

	// 6. Test status with an existing session (covers truncateString and session printing)
	sm := transfer.NewSessionManager(consumerDir)
	_, _ = sm.CreateOrLoadSession("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"very_long_filename_that_exceeds_twenty_characters.txt",
		1000000, 524288, 2, "peer123", "127.0.0.1:4001", "downloads/file.txt")
	stdout.Reset()
	stderr.Reset()
	code = Run([]string{"status", "--data-dir", consumerDir}, &stdout, &stderr)
	if code != ExitSuccess {
		t.Errorf("expected status success, got %d", code)
	}
	if !strings.Contains(stdout.String(), "very_long_file") {
		t.Errorf("expected truncated filename in status output, got: %s", stdout.String())
	}

	// 7. WriteJSONError direct test
	stdout.Reset()
	WriteJSONError(&stdout, "test", "simulated error", ExitValidation)
	if !strings.Contains(stdout.String(), "simulated error") {
		t.Errorf("expected JSON error output, got %s", stdout.String())
	}
}

func TestCLI_StartValidation(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"start", "--invalid-flag"}, &stdout, &stderr)
	if code != ExitValidation {
		t.Errorf("expected ExitValidation (2) for invalid flag, got %d", code)
	}
}
