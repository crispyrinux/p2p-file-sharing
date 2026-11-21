package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"p2p-file-sharing/pkg/manifest"
)

// ShareRecord tracks local shared file mappings.
type ShareRecord struct {
	FileID       string `json:"file_id"`
	OriginalPath string `json:"original_path"`
	ManifestPath string `json:"manifest_path"`
}

// RunShare executes the `p2p share` command.
func RunShare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("share", flag.ContinueOnError)
	fs.SetOutput(stderr)

	chunkSize := fs.Uint("chunk-size", uint(manifest.DefaultChunkSize), "Chunk size in bytes (64 KB to 2 MB)")
	dataDir := fs.String("data-dir", ".p2p", "Working directory for node data")
	jsonFlag := fs.Bool("json", false, "Output in JSON format")

	remaining, err := ParseFlags(fs, args)
	if err != nil {
		return ExitValidation
	}

	if len(remaining) == 0 {
		if *jsonFlag {
			WriteJSONError(stdout, "share", "file path argument is required", ExitValidation)
		} else {
			fmt.Fprintln(stderr, "Error: file path argument is required")
			fmt.Fprintln(stderr, "Usage: p2p share <path/to/file> [--chunk-size <bytes>] [--data-dir <path>] [--json]")
		}
		return ExitValidation
	}

	filePath := remaining[0]
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "share", fmt.Sprintf("invalid file path: %v", err), ExitValidation)
		} else {
			fmt.Fprintf(stderr, "Error: invalid file path: %v\n", err)
		}
		return ExitValidation
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "share", fmt.Sprintf("file not accessible: %v", err), ExitValidation)
		} else {
			fmt.Fprintf(stderr, "Error: file not accessible: %v\n", err)
		}
		return ExitValidation
	}

	if stat.IsDir() {
		if *jsonFlag {
			WriteJSONError(stdout, "share", "directory sharing is not supported in MVP", ExitValidation)
		} else {
			fmt.Fprintln(stderr, "Error: directory sharing is not supported in MVP")
		}
		return ExitValidation
	}

	fileSize := uint64(stat.Size())
	if fileSize < manifest.MinFileSize || fileSize > manifest.MaxFileSize {
		errStr := fmt.Sprintf("file size %d bytes out of supported range (1 KB to 2 GB)", fileSize)
		if *jsonFlag {
			WriteJSONError(stdout, "share", errStr, ExitValidation)
		} else {
			fmt.Fprintf(stderr, "Error: %s\n", errStr)
		}
		return ExitValidation
	}

	m, err := manifest.CreateManifest(absPath, uint32(*chunkSize))
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "share", fmt.Sprintf("failed to create manifest: %v", err), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error: failed to create manifest: %v\n", err)
		}
		return ExitRuntime
	}

	// Validate manifest immediately
	if err := manifest.ValidateManifest(m, m.FileID); err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "share", fmt.Sprintf("manifest validation failed: %v", err), ExitValidation)
		} else {
			fmt.Fprintf(stderr, "Error: manifest validation failed: %v\n", err)
		}
		return ExitValidation
	}

	// Save manifest file
	manifestDir := filepath.Join(*dataDir, "manifests")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "share", fmt.Sprintf("failed to create manifests directory: %v", err), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error: %v\n", err)
		}
		return ExitRuntime
	}

	manifestPath := filepath.Join(manifestDir, m.FileID+".json")
	mJSON, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return ExitRuntime
	}
	if err := os.WriteFile(manifestPath, mJSON, 0644); err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "share", fmt.Sprintf("failed to save manifest: %v", err), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error saving manifest: %v\n", err)
		}
		return ExitRuntime
	}

	// Persist to shares.json
	_ = recordShare(*dataDir, m.FileID, absPath, manifestPath)

	if *jsonFlag {
		WriteJSON(stdout, "success", "share", map[string]interface{}{
			"file_id":       m.FileID,
			"file_name":     m.FileName,
			"total_bytes":   m.TotalBytes,
			"chunk_size":    m.ChunkSize,
			"chunk_count":   m.ChunkCount,
			"manifest_path": manifestPath,
		})
	} else {
		fmt.Fprintln(stdout, "File published successfully.")
		fmt.Fprintf(stdout, "File Name:   %s\n", m.FileName)
		fmt.Fprintf(stdout, "File Size:   %d bytes (%.1f MB)\n", m.TotalBytes, float64(m.TotalBytes)/(1024*1024))
		fmt.Fprintf(stdout, "File ID:     %s\n", m.FileID)
		fmt.Fprintf(stdout, "Chunk Count: %d (%d bytes each)\n", m.ChunkCount, m.ChunkSize)
		fmt.Fprintf(stdout, "Share Token: %s\n", m.FileID)
	}

	return ExitSuccess
}

func recordShare(dataDir, fileID, originalPath, manifestPath string) error {
	sharesFile := filepath.Join(dataDir, "shares.json")
	var records []ShareRecord

	data, err := os.ReadFile(sharesFile)
	if err == nil {
		_ = json.Unmarshal(data, &records)
	}

	for _, r := range records {
		if r.FileID == fileID {
			return nil
		}
	}

	records = append(records, ShareRecord{
		FileID:       fileID,
		OriginalPath: originalPath,
		ManifestPath: manifestPath,
	})

	encoded, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sharesFile, encoded, 0644)
}
