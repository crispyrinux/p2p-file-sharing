package cli

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"p2p-file-sharing/pkg/identity"
)

// RunInit executes the `p2p init` command.
func RunInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)

	dataDir := fs.String("data-dir", ".p2p", "Working directory for node configuration and identity")
	jsonFlag := fs.Bool("json", false, "Output in JSON format")

	if err := fs.Parse(args); err != nil {
		return ExitValidation
	}

	configPath := filepath.Join(*dataDir, "identity.json")
	id, created, err := identity.LoadOrInit(configPath)
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "init", err.Error(), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error initializing node identity: %v\n", err)
		}
		return ExitRuntime
	}

	if *jsonFlag {
		WriteJSON(stdout, "success", "init", map[string]interface{}{
			"peer_id":     id.PeerID,
			"config_path": configPath,
			"created_at":  id.CreatedAt.Format(time.RFC3339),
			"new":         created,
		})
	} else {
		if created {
			fmt.Fprintln(stdout, "Initialized new node identity.")
		} else {
			fmt.Fprintln(stdout, "Existing node identity loaded.")
		}
		fmt.Fprintf(stdout, "Peer ID: %s\n", id.PeerID)
		fmt.Fprintf(stdout, "Config saved to: %s\n", configPath)
	}

	return ExitSuccess
}
