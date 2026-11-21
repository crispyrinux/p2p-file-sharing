package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
)

// Standard CLI Exit Codes per contract.
const (
	ExitSuccess    = 0
	ExitRuntime    = 1
	ExitValidation = 2
	ExitNetwork    = 3
	ExitIntegrity  = 4
)

// JSONResponse represents standard structured output for CLI when --json is passed.
type JSONResponse struct {
	Status  string      `json:"status"`
	Command string      `json:"command"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Code    int         `json:"code,omitempty"`
}

// WriteJSON outputs a JSONResponse to the given writer.
func WriteJSON(w io.Writer, status, command string, data interface{}) {
	resp := JSONResponse{
		Status:  status,
		Command: command,
		Data:    data,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}

// WriteJSONError outputs a JSON error response.
func WriteJSONError(w io.Writer, command string, errMsg string, code int) {
	resp := JSONResponse{
		Status:  "error",
		Command: command,
		Error:   errMsg,
		Code:    code,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(resp)
}

// ParseFlags allows flags and positional arguments in any order.
func ParseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, nil
}

// Run executes CLI subcommands with custom streams for testing and CLI usage.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: p2p <command> [options]")
		fmt.Fprintln(stderr, "Available commands: init, share, download, peers, status, start")
		return ExitValidation
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "init":
		return RunInit(cmdArgs, stdout, stderr)
	case "share":
		return RunShare(cmdArgs, stdout, stderr)
	case "download":
		return RunDownload(cmdArgs, stdout, stderr)
	case "peers":
		return RunPeers(cmdArgs, stdout, stderr)
	case "status":
		return RunStatus(cmdArgs, stdout, stderr)
	case "start":
		return RunStart(cmdArgs, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", cmd)
		return ExitValidation
	}
}
