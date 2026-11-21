package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"p2p-file-sharing/pkg/discovery"
	"p2p-file-sharing/pkg/identity"
	"p2p-file-sharing/pkg/manifest"
	"p2p-file-sharing/pkg/transfer"
	"p2p-file-sharing/pkg/transport"
	"p2p-file-sharing/pkg/web"
)

// DaemonInfo tracks runtime parameters for running nodes.
type DaemonInfo struct {
	PeerID    string    `json:"peer_id"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

// RunStart executes the `p2p start` command, launching the background peer daemon.
func RunStart(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(stderr)

	port := fs.Int("port", 4001, "Local TCP listening port")
	webPort := fs.Int("web", 8080, "Embedded Web UI HTTP port (0 to disable)")
	noWeb := fs.Bool("no-web", false, "Disable embedded Web UI dashboard")
	dataDir := fs.String("data-dir", ".p2p", "Working directory for node data")
	noMDNS := fs.Bool("no-mdns", false, "Disable mDNS broadcast")
	jsonFlag := fs.Bool("json", false, "Output in JSON format")

	_, err := ParseFlags(fs, args)
	if err != nil {
		return ExitValidation
	}

	configPath := filepath.Join(*dataDir, "identity.json")
	id, _, err := identity.LoadOrInit(configPath)
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "start", fmt.Sprintf("failed to load identity: %v", err), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error loading node identity: %v\n", err)
		}
		return ExitRuntime
	}

	tlsConf, err := transport.NewServerTLSConfig(id)
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "start", fmt.Sprintf("failed to configure TLS: %v", err), ExitRuntime)
		} else {
			fmt.Fprintf(stderr, "Error configuring TLS: %v\n", err)
		}
		return ExitRuntime
	}

	addr := fmt.Sprintf("0.0.0.0:%d", *port)
	listener, err := transport.ListenTLS(addr, tlsConf)
	if err != nil {
		if *jsonFlag {
			WriteJSONError(stdout, "start", fmt.Sprintf("failed to bind port %d: %v", *port, err), ExitNetwork)
		} else {
			fmt.Fprintf(stderr, "Error: cannot bind to port %d: %v\n", *port, err)
		}
		return ExitNetwork
	}
	defer listener.Close()

	staging := transfer.NewStagingManager(*dataDir)
	engine := transfer.NewTransferEngine(staging)

	// Load previously shared files
	loadSharedFiles(*dataDir, engine)

	// Save daemon info
	dInfo := DaemonInfo{
		PeerID:    id.PeerID,
		Port:      *port,
		StartedAt: time.Now().UTC(),
	}
	dBytes, _ := json.MarshalIndent(dInfo, "", "  ")
	_ = os.WriteFile(filepath.Join(*dataDir, "daemon.json"), dBytes, 0644)

	// Peer registry
	reg := discovery.NewPeerRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mdnsSvc *discovery.MDNSService
	if !*noMDNS {
		svc, err := discovery.StartMDNSService(id.PeerID, *port)
		if err == nil {
			mdnsSvc = svc
			_ = discovery.DiscoverPeers(ctx, reg, id.PeerID)
		}
	}

	// Persist peers periodically
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				peers := reg.ListPeers()
				pBytes, _ := json.MarshalIndent(peers, "", "  ")
				_ = os.WriteFile(filepath.Join(*dataDir, "peers.json"), pBytes, 0644)
			}
		}
	}()

	// Web UI Server
	var webServer *web.Server
	if !*noWeb && *webPort > 0 {
		webCfg := web.ServerConfig{
			Port:       uint16(*webPort),
			P2PPort:    uint16(*port),
			PeerID:     id.PeerID,
			DataDir:    *dataDir,
			MDNSActive: !*noMDNS,
			Registry:   reg,
			Engine:     engine,
			Identity:   id,
			StartTime:  time.Now(),
		}
		ws, err := web.NewServer(webCfg, web.DefaultAssets())
		if err == nil {
			webServer = ws
			go func() {
				if err := webServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					fmt.Fprintf(stderr, "Web server warning: %v\n", err)
				}
			}()
		}
	}

	if *jsonFlag {
		jsonMap := map[string]interface{}{
			"peer_id": id.PeerID,
			"port":    *port,
			"status":  "running",
			"mdns":    !*noMDNS,
		}
		if webServer != nil {
			jsonMap["web_port"] = *webPort
		}
		WriteJSON(stdout, "success", "start", jsonMap)
	} else {
		fmt.Fprintf(stdout, "Node daemon running on port %d.\n", *port)
		fmt.Fprintf(stdout, "Peer ID: %s\n", id.PeerID)
		if webServer != nil {
			fmt.Fprintf(stdout, "Web UI Dashboard: http://localhost:%d\n", *webPort)
		}
		if !*noMDNS {
			fmt.Fprintln(stdout, "mDNS local discovery active.")
		}
		fmt.Fprintln(stdout, "Press Ctrl+C to terminate.")
	}

	// Trap termination signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		if !*jsonFlag {
			fmt.Fprintln(stdout, "\nShutting down daemon...")
		}
		cancel()
		if webServer != nil {
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer shutCancel()
			_ = webServer.Shutdown(shutCtx)
		}
		if mdnsSvc != nil {
			mdnsSvc.Shutdown()
		}
		_ = listener.Close()
		_ = os.Remove(filepath.Join(*dataDir, "daemon.json"))
	}()

	// Accept connections loop
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ExitSuccess
			default:
				return ExitNetwork
			}
		}
		go func() {
			_ = engine.HandleConnection(conn)
		}()
	}
}

func loadSharedFiles(dataDir string, engine *transfer.TransferEngine) {
	sharesFile := filepath.Join(dataDir, "shares.json")
	data, err := os.ReadFile(sharesFile)
	if err != nil {
		return
	}

	var records []ShareRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return
	}

	for _, rec := range records {
		mBytes, err := os.ReadFile(rec.ManifestPath)
		if err != nil {
			continue
		}
		var m manifest.FileManifest
		if err := json.Unmarshal(mBytes, &m); err != nil {
			continue
		}
		_ = engine.RegisterPublishedFile(&m, rec.OriginalPath)
	}
}
