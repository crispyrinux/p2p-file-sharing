package cli

import (
	"flag"
	"fmt"
	"io"

	"p2p-file-sharing/pkg/transfer"
)

// RunStatus executes the `p2p status` command inspecting active, paused, and interrupted sessions.
func RunStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)

	sessionID := fs.String("session-id", "", "Filter by specific session ID")
	dataDir := fs.String("data-dir", ".p2p", "Working directory for node data")
	jsonFlag := fs.Bool("json", false, "Output in JSON format")

	_, err := ParseFlags(fs, args)
	if err != nil {
		return ExitValidation
	}

	sm := transfer.NewSessionManager(*dataDir)
	sessions, err := sm.ListSessions()
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "status", err.Error(), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error reading transfer sessions: %v\n", err)
		}
		return ExitRuntime
	}

	var filtered []*transfer.TransferSession
	for _, s := range sessions {
		if *sessionID == "" || s.SessionID == *sessionID || s.FileID == *sessionID {
			filtered = append(filtered, s)
		}
	}

	if *jsonFlag {
		WriteJSON(stdout, "success", "status", filtered)
		return ExitSuccess
	}

	if len(filtered) == 0 {
		fmt.Fprintln(stdout, "No active or paused transfer sessions.")
		return ExitSuccess
	}

	fmt.Fprintf(stdout, "%-36s  %-20s  %-12s  %-12s  %s\n", "SESSION ID", "FILE NAME", "PROGRESS", "STATUS", "REMOTE PEER")
	for _, s := range filtered {
		pct := 0.0
		if s.TotalChunks > 0 {
			pct = float64(s.CompletedChunks) / float64(s.TotalChunks) * 100.0
		}
		progressStr := fmt.Sprintf("%d/%d (%.0f%%)", s.CompletedChunks, s.TotalChunks, pct)
		fmt.Fprintf(stdout, "%-36s  %-20s  %-12s  %-12s  %s\n",
			s.SessionID,
			truncateString(s.FileName, 20),
			progressStr,
			s.State,
			s.RemoteEndpoint,
		)
	}

	return ExitSuccess
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
