package cli

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"p2p-file-sharing/pkg/identity"
	"p2p-file-sharing/pkg/manifest"
	"p2p-file-sharing/pkg/transfer"
	"p2p-file-sharing/pkg/transport"
)

// RunDownload executes the `p2p download` command.
func RunDownload(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	fs.SetOutput(stderr)

	fileID := fs.String("file-id", "", "32-byte SHA-256 File ID (64 hex characters)")
	peer := fs.String("peer", "", "Remote provider endpoint (host:port)")
	outputDir := fs.String("output-dir", "downloads", "Target directory for assembled file")
	dataDir := fs.String("data-dir", ".p2p", "Working directory for node data")
	jsonFlag := fs.Bool("json", false, "Output in JSON format")
	_, err := ParseFlags(fs, args)
	if err != nil {
		return ExitValidation
	}

	if *fileID == "" || *peer == "" {
		if *jsonFlag {
			WriteJSONError(stdout, "download", "both --file-id and --peer flags are required", ExitValidation)
		} else {
			fmt.Fprintln(stderr, "Error: both --file-id and --peer flags are required")
			fmt.Fprintln(stderr, "Usage: p2p download --file-id <file-id> --peer <ip:port> [--output-dir <path>] [--data-dir <path>] [--json]")
		}
		return ExitValidation
	}

	rawFID, err := hex.DecodeString(*fileID)
	if err != nil || len(rawFID) != 32 {
		if *jsonFlag {
			WriteJSONError(stdout, "download", "invalid file ID: must be 64 hexadecimal characters", ExitValidation)
		} else {
			fmt.Fprintf(stderr, "Error: invalid file ID '%s' (must be 64 hexadecimal characters)\n", *fileID)
		}
		return ExitValidation
	}

	// Prepare data directory and identity
	configPath := filepath.Join(*dataDir, "identity.json")
	id, _, err := identity.LoadOrInit(configPath)
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "download", fmt.Sprintf("failed to load node identity: %v", err), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error loading node identity: %v\n", err)
		}
		return ExitRuntime
	}

	// TLS client config
	tlsConfig, err := transport.NewClientTLSConfig(id, "")
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "download", fmt.Sprintf("failed to configure TLS: %v", err), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error configuring TLS: %v\n", err)
		}
		return ExitRuntime
	}

	if !*jsonFlag {
		fmt.Fprintf(stdout, "Connecting to peer %s over encrypted channel... ", *peer)
	}

	startTime := time.Now()
	conn, err := transport.DialTLS(*peer, tlsConfig, 10*time.Second)
	if err != nil {
		if !*jsonFlag {
			fmt.Fprintln(stdout, "Failed.")
		}
		if *jsonFlag {
			WriteJSONError(stdout, "download", fmt.Sprintf("network connection error: %v", err), ExitNetwork)
		} else {
			fmt.Fprintf(stderr, "Error: unable to connect to peer %s: %v\n", *peer, err)
		}
		return ExitNetwork
	}
	defer conn.Close()

	if !*jsonFlag {
		fmt.Fprintln(stdout, "Connected.")
	}

	staging := transfer.NewStagingManager(*dataDir)
	engine := transfer.NewTransferEngine(staging)

	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "download", fmt.Sprintf("failed to create output directory: %v", err), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error creating output directory: %v\n", err)
		}
		return ExitRuntime
	}

	targetPath := filepath.Join(*outputDir, *fileID+".bin") // fallback target path
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var totalChunks uint32
	var completedChunks uint32

	progressCb := func(completed, total uint32) {
		completedChunks = completed
		totalChunks = total
		if !*jsonFlag {
			pct := float64(completed) / float64(total) * 100.0
			fmt.Fprintf(stdout, "\rDownloading chunks: %d/%d (%.1f%%)", completed, total, pct)
		}
	}

	err = engine.DownloadFile(ctx, conn, *fileID, targetPath, progressCb)
	if err != nil {
		if !*jsonFlag {
			fmt.Fprintln(stdout)
		}
		var code int
		if errors.Is(err, manifest.ErrFileSizeOutOfBounds) || errors.Is(err, manifest.ErrChunkSizeOutOfBounds) ||
			errors.Is(err, manifest.ErrChunkCountMismatch) || errors.Is(err, manifest.ErrChunkHashesLength) {
			code = ExitValidation
		} else if strings.Contains(err.Error(), "mismatch") || strings.Contains(err.Error(), "SHA-256") || strings.Contains(err.Error(), "checksum") {
			code = ExitIntegrity
		} else if strings.Contains(err.Error(), "connection") || strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "network") {
			code = ExitNetwork
		} else {
			code = ExitRuntime
		}

		if *jsonFlag {
			WriteJSONError(stdout, "download", err.Error(), code)
		} else {
			fmt.Fprintf(stderr, "Download failed: %v\n", err)
		}
		return code
	}

	duration := time.Since(startTime)
	if !*jsonFlag {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Post-assembly SHA-256 verification: MATCH")
		fmt.Fprintf(stdout, "File saved to: %s (Transfer Completed)\n", targetPath)
	} else {
		WriteJSON(stdout, "success", "download", map[string]interface{}{
			"file_id":           *fileID,
			"target_path":       targetPath,
			"chunks_completed":  completedChunks,
			"total_chunks":      totalChunks,
			"checksum_verified": true,
			"duration_ms":       duration.Milliseconds(),
		})
	}

	return ExitSuccess
}
