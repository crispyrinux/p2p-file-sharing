package web

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"p2p-file-sharing/pkg/discovery"
	"p2p-file-sharing/pkg/identity"
	"p2p-file-sharing/pkg/manifest"
	"p2p-file-sharing/pkg/transfer"
	"p2p-file-sharing/pkg/transport"
)

// ServerConfig holds configuration and engine dependencies for the Web UI server.
type ServerConfig struct {
	Port       uint16
	P2PPort    uint16
	PeerID     string
	DataDir    string
	MDNSActive bool
	Registry   *discovery.PeerRegistry
	Engine     *transfer.TransferEngine
	Identity   *identity.PeerIdentity
	StartTime  time.Time
}

// Server provides the embedded HTTP server and API endpoints for the dashboard.
type Server struct {
	cfg        ServerConfig
	assets     fs.FS
	hub        *EventHub
	mux        *http.ServeMux
	httpServer *http.Server
	activeJobs sync.Map // map[string]context.CancelFunc
}

// NewServer creates an initialized Web UI Server.
func NewServer(cfg ServerConfig, assets fs.FS) (*Server, error) {
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.StartTime.IsZero() {
		cfg.StartTime = time.Now()
	}

	s := &Server{
		cfg:    cfg,
		assets: assets,
		hub:    NewEventHub(),
		mux:    http.NewServeMux(),
	}

	s.registerRoutes()
	return s, nil
}

// Handler returns the HTTP handler for testing.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// EventHub returns the SSE event hub.
func (s *Server) EventHub() *EventHub {
	return s.hub
}

// Start begins listening and serving HTTP requests.
func (s *Server) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%d", s.cfg.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // Infinite for SSE streaming
		IdleTimeout:  60 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("web server cannot bind to %s: %w", addr, err)
	}

	return s.httpServer.Serve(ln)
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.hub.Close()
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) registerRoutes() {
	// REST API Endpoints
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/peers", s.handleGetPeers)
	s.mux.HandleFunc("POST /api/peers", s.handleAddPeer)
	s.mux.HandleFunc("GET /api/shares", s.handleGetShares)
	s.mux.HandleFunc("POST /api/share", s.handleShareFile)
	s.mux.HandleFunc("GET /api/transfers", s.handleGetTransfers)
	s.mux.HandleFunc("POST /api/download", s.handleStartDownload)
	s.mux.HandleFunc("POST /api/transfers/{id}/pause", s.handlePauseTransfer)
	s.mux.HandleFunc("POST /api/transfers/{id}/resume", s.handleResumeTransfer)

	// SSE Telemetry Stream
	s.mux.Handle("GET /api/events", s.hub)

	// Static Assets
	s.mux.HandleFunc("GET /", s.handleStaticAssets)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	uptime := int64(time.Since(s.cfg.StartTime).Seconds())
	status := NodeStatusView{
		PeerID:        s.cfg.PeerID,
		Port:          s.cfg.P2PPort,
		WebPort:       s.cfg.Port,
		DataDir:       s.cfg.DataDir,
		MDNSActive:    s.cfg.MDNSActive,
		UptimeSeconds: uptime,
		Version:       "1.0.0",
	}

	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleGetPeers(w http.ResponseWriter, r *http.Request) {
	var peers []PeerView
	if s.cfg.Registry != nil {
		records := s.cfg.Registry.ListPeers()
		for _, rec := range records {
			peers = append(peers, PeerView{
				PeerID:    rec.PeerID,
				Endpoint:  rec.Endpoint,
				Source:    string(rec.Source),
				Status:    string(rec.Status),
				LatencyMS: rec.LatencyMs,
				LastSeen:  rec.LastSeen,
			})
		}
	}
	if peers == nil {
		peers = []PeerView{}
	}
	writeJSON(w, http.StatusOK, peers)
}

func (s *Server) handleAddPeer(w http.ResponseWriter, r *http.Request) {
	var req AddPeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Endpoint) == "" {
		http.Error(w, `{"error":"invalid endpoint"}`, http.StatusBadRequest)
		return
	}

	endpoint := strings.TrimSpace(req.Endpoint)
	if s.cfg.Registry != nil {
		s.cfg.Registry.RegisterPeer(endpoint, endpoint, discovery.DiscoveryManual)
	}

	s.hub.Broadcast(TelemetryEvent{
		Type:      "peer_update",
		Endpoint:  endpoint,
		Status:    "Connected",
		Timestamp: time.Now().Unix(),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"status":   "success",
		"message":  "Peer endpoint registered",
		"endpoint": endpoint,
	})
}

type shareRecord struct {
	FileID       string `json:"file_id"`
	OriginalPath string `json:"original_path"`
	ManifestPath string `json:"manifest_path"`
}

func (s *Server) handleGetShares(w http.ResponseWriter, r *http.Request) {
	sharesFile := filepath.Join(s.cfg.DataDir, "shares.json")
	var records []shareRecord
	data, err := os.ReadFile(sharesFile)
	if err == nil {
		_ = json.Unmarshal(data, &records)
	}

	shares := make([]FileShareView, 0, len(records))
	for _, rec := range records {
		mBytes, err := os.ReadFile(rec.ManifestPath)
		if err != nil {
			continue
		}
		var m manifest.FileManifest
		if err := json.Unmarshal(mBytes, &m); err != nil {
			continue
		}
		shares = append(shares, FileShareView{
			FileID:     m.FileID,
			FileName:   m.FileName,
			TotalBytes: int64(m.TotalBytes),
			ChunkSize:  int64(m.ChunkSize),
			ChunkCount: int(m.ChunkCount),
			CreatedAt:  m.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, shares)
}

func (s *Server) handleShareFile(w http.ResponseWriter, r *http.Request) {
	var req ShareFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.FilePath) == "" {
		http.Error(w, `{"error":"file_path is required"}`, http.StatusBadRequest)
		return
	}

	absPath, err := filepath.Abs(req.FilePath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid path: %v"}`, err), http.StatusBadRequest)
		return
	}

	stat, err := os.Stat(absPath)
	if err != nil || stat.IsDir() {
		http.Error(w, `{"error":"file does not exist or is directory"}`, http.StatusBadRequest)
		return
	}

	m, err := manifest.CreateManifest(absPath, manifest.DefaultChunkSize)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to create manifest: %v"}`, err), http.StatusInternalServerError)
		return
	}

	if err := manifest.ValidateManifest(m, m.FileID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"manifest validation failed: %v"}`, err), http.StatusInternalServerError)
		return
	}

	manifestDir := filepath.Join(s.cfg.DataDir, "manifests")
	_ = os.MkdirAll(manifestDir, 0755)
	manifestPath := filepath.Join(manifestDir, m.FileID+".json")
	mJSON, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(manifestPath, mJSON, 0644)

	// Persist to shares.json
	sharesFile := filepath.Join(s.cfg.DataDir, "shares.json")
	var records []shareRecord
	data, _ := os.ReadFile(sharesFile)
	_ = json.Unmarshal(data, &records)
	records = append(records, shareRecord{
		FileID:       m.FileID,
		OriginalPath: absPath,
		ManifestPath: manifestPath,
	})
	recBytes, _ := json.MarshalIndent(records, "", "  ")
	_ = os.WriteFile(sharesFile, recBytes, 0644)

	// Register with TransferEngine if available
	if s.cfg.Engine != nil {
		_ = s.cfg.Engine.RegisterPublishedFile(m, absPath)
	}

	view := FileShareView{
		FileID:     m.FileID,
		FileName:   m.FileName,
		TotalBytes: int64(m.TotalBytes),
		ChunkSize:  int64(m.ChunkSize),
		ChunkCount: int(m.ChunkCount),
		CreatedAt:  m.CreatedAt,
	}

	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) handleGetTransfers(w http.ResponseWriter, r *http.Request) {
	var views []TransferSessionView
	if s.cfg.Engine != nil && s.cfg.Engine.SessionManager() != nil {
		sessions, _ := s.cfg.Engine.SessionManager().ListSessions()
		for _, sess := range sessions {
			views = append(views, TransferSessionView{
				SessionID:       sess.SessionID,
				FileID:          sess.FileID,
				FileName:        sess.FileName,
				TotalBytes:      int64(sess.TotalBytes),
				ChunkSize:       int64(sess.ChunkSize),
				TotalChunks:     int(sess.TotalChunks),
				CompletedChunks: int(sess.CompletedChunks),
				State:           string(sess.State),
				RemoteEndpoint:  sess.RemoteEndpoint,
				Bitfield:        sess.Bitfield,
			})
		}
	}
	if views == nil {
		views = []TransferSessionView{}
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) handleStartDownload(w http.ResponseWriter, r *http.Request) {
	var req DownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if len(req.FileID) != 64 {
		http.Error(w, `{"error":"file_id must be 64 hex characters"}`, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Peer) == "" {
		http.Error(w, `{"error":"peer endpoint is required"}`, http.StatusBadRequest)
		return
	}

	if req.OutputDir == "" {
		req.OutputDir = filepath.Join(s.cfg.DataDir, "downloads")
	}
	_ = os.MkdirAll(req.OutputDir, 0755)

	targetPath := filepath.Join(req.OutputDir, req.FileID+".bin")

	// Trigger download asynchronously
	if s.cfg.Engine != nil {
		ctx, cancel := context.WithCancel(context.Background())
		s.activeJobs.Store(req.FileID, cancel)

		go func() {
			defer s.activeJobs.Delete(req.FileID)
			s.hub.Broadcast(TelemetryEvent{
				Type:      "transfer_state",
				FileID:    req.FileID,
				State:     "Active",
				Timestamp: time.Now().Unix(),
			})

			// Establish TLS connection to peer and download
			var tlsConfig *tls.Config
			var err error
			if s.cfg.Identity != nil {
				tlsConfig, err = transport.NewClientTLSConfig(s.cfg.Identity, "")
			} else {
				id, _, idErr := identity.LoadOrInit(filepath.Join(s.cfg.DataDir, "identity.json"))
				if idErr == nil {
					tlsConfig, err = transport.NewClientTLSConfig(id, "")
				}
			}

			if tlsConfig == nil || err != nil {
				s.hub.Broadcast(TelemetryEvent{
					Type:      "transfer_state",
					FileID:    req.FileID,
					State:     "Failed",
					Timestamp: time.Now().Unix(),
				})
				return
			}

			conn, err := transport.DialTLS(req.Peer, tlsConfig, 10*time.Second)
			if err != nil {
				s.hub.Broadcast(TelemetryEvent{
					Type:      "transfer_state",
					FileID:    req.FileID,
					State:     "Failed",
					Timestamp: time.Now().Unix(),
				})
				return
			}
			defer conn.Close()

			startTime := time.Now()
			lastBytes := uint64(0)

			err = s.cfg.Engine.DownloadFile(ctx, conn, req.FileID, targetPath, func(completed, total uint32) {
				elapsed := time.Since(startTime).Seconds()
				var speedMbps float64
				if elapsed > 0 {
					transferred := uint64(completed) * uint64(manifest.DefaultChunkSize)
					if transferred > lastBytes {
						speedMbps = float64(transferred) / (1024 * 1024 * elapsed)
					}
				}

				s.hub.Broadcast(TelemetryEvent{
					Type:            "chunk_verified",
					FileID:          req.FileID,
					Index:           int(completed - 1),
					TotalChunks:     int(total),
					CompletedChunks: int(completed),
					SpeedMbps:       speedMbps,
					Timestamp:       time.Now().Unix(),
				})
			})

			if err != nil {
				s.hub.Broadcast(TelemetryEvent{
					Type:      "transfer_state",
					FileID:    req.FileID,
					State:     "Interrupted",
					Timestamp: time.Now().Unix(),
				})
			} else {
				s.hub.Broadcast(TelemetryEvent{
					Type:       "transfer_state",
					FileID:     req.FileID,
					State:      "Completed",
					TargetPath: targetPath,
					Timestamp:  time.Now().Unix(),
				})
			}
		}()
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":     "initiated",
		"file_id":    req.FileID,
		"peer":       req.Peer,
		"target_dir": req.OutputDir,
	})
}

func (s *Server) handlePauseTransfer(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
		return
	}

	if cancelVal, ok := s.activeJobs.Load(fileID); ok {
		if cancel, isCancel := cancelVal.(context.CancelFunc); isCancel {
			cancel()
		}
		s.activeJobs.Delete(fileID)
	}

	if s.cfg.Engine != nil && s.cfg.Engine.SessionManager() != nil {
		_ = s.cfg.Engine.SessionManager().UpdateState(fileID, transfer.StateInterrupted)
	}

	s.hub.Broadcast(TelemetryEvent{
		Type:      "transfer_state",
		FileID:    fileID,
		State:     "Paused",
		Timestamp: time.Now().Unix(),
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "paused", "file_id": fileID})
}

func (s *Server) handleResumeTransfer(w http.ResponseWriter, r *http.Request) {
	fileID := r.PathValue("id")
	if fileID == "" {
		http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
		return
	}

	if s.cfg.Engine != nil && s.cfg.Engine.SessionManager() != nil {
		sess, err := s.cfg.Engine.SessionManager().LoadSession(fileID)
		if err == nil {
			_ = s.cfg.Engine.SessionManager().UpdateState(fileID, transfer.StateActive)
			s.hub.Broadcast(TelemetryEvent{
				Type:      "transfer_state",
				FileID:    fileID,
				State:     "Active",
				Timestamp: time.Now().Unix(),
			})
			writeJSON(w, http.StatusOK, map[string]string{"status": "resumed", "file_id": fileID, "peer": sess.RemoteEndpoint})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed", "file_id": fileID})
}

func (s *Server) handleStaticAssets(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	file, err := s.assets.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
