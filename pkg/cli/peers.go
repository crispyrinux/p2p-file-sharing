package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"p2p-file-sharing/pkg/discovery"
)

// RunPeers executes the `p2p peers` command listing known peers.
func RunPeers(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("peers", flag.ContinueOnError)
	fs.SetOutput(stderr)

	dataDir := fs.String("data-dir", ".p2p", "Working directory for node data")
	addEndpoint := fs.String("add", "", "Manually register a peer endpoint (host:port)")
	jsonFlag := fs.Bool("json", false, "Output in JSON format")

	_, err := ParseFlags(fs, args)
	if err != nil {
		return ExitValidation
	}

	peersFile := filepath.Join(*dataDir, "peers.json")
	var peers []*discovery.PeerRecord

	if data, err := os.ReadFile(peersFile); err == nil {
		_ = json.Unmarshal(data, &peers)
	}

	if *addEndpoint != "" {
		newPeer := &discovery.PeerRecord{
			PeerID:      "manual-" + *addEndpoint,
			Endpoint:    *addEndpoint,
			Source:      discovery.DiscoveryManual,
			Status:      discovery.PeerStatusConnected,
			LastSeen:    time.Now().UTC(),
			FailedPings: 0,
		}
		peers = append(peers, newPeer)
		_ = os.MkdirAll(*dataDir, 0755)
		data, _ := json.MarshalIndent(peers, "", "  ")
		_ = os.WriteFile(peersFile, data, 0644)
	}

	if *jsonFlag {
		WriteJSON(stdout, "success", "peers", peers)
		return ExitSuccess
	}

	if len(peers) == 0 {
		fmt.Fprintln(stdout, "No peers discovered or connected.")
		return ExitSuccess
	}

	fmt.Fprintf(stdout, "%-64s  %-21s  %-7s  %-10s  %s\n", "PEER ID", "ENDPOINT", "SOURCE", "STATUS", "LATENCY")
	for _, p := range peers {
		latencyStr := "-"
		if p.LatencyMs > 0 {
			latencyStr = fmt.Sprintf("%dms", p.LatencyMs)
		}
		fmt.Fprintf(stdout, "%-64s  %-21s  %-7s  %-10s  %s\n",
			p.PeerID,
			p.Endpoint,
			p.Source,
			p.Status,
			latencyStr,
		)
	}

	return ExitSuccess
}
