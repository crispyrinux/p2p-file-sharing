package web

import "time"

// NodeStatusView represents node status returned by GET /api/status.
type NodeStatusView struct {
	PeerID        string `json:"peer_id"`
	Port          uint16 `json:"port"`
	WebPort       uint16 `json:"web_port"`
	DataDir       string `json:"data_dir"`
	MDNSActive    bool   `json:"mdns_active"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	Version       string `json:"version"`
}

// PeerView represents a discovered or connected peer returned by GET /api/peers.
type PeerView struct {
	PeerID    string    `json:"peer_id"`
	Endpoint  string    `json:"endpoint"`
	Source    string    `json:"source"`
	Status    string    `json:"status"`
	LatencyMS int64     `json:"latency_ms"`
	LastSeen  time.Time `json:"last_seen"`
}

// AddPeerRequest represents request body for POST /api/peers.
type AddPeerRequest struct {
	Endpoint string `json:"endpoint"`
}

// FileShareView represents a locally shared file returned by GET /api/shares.
type FileShareView struct {
	FileID     string    `json:"file_id"`
	FileName   string    `json:"file_name"`
	TotalBytes int64     `json:"total_bytes"`
	ChunkSize  int64     `json:"chunk_size"`
	ChunkCount int       `json:"chunk_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ShareFileRequest represents request body for POST /api/share.
type ShareFileRequest struct {
	FilePath string `json:"file_path"`
}

// TransferSessionView represents an active or past transfer session.
type TransferSessionView struct {
	SessionID       string   `json:"session_id"`
	FileID          string   `json:"file_id"`
	FileName        string   `json:"file_name"`
	TotalBytes      int64    `json:"total_bytes"`
	ChunkSize       int64    `json:"chunk_size"`
	TotalChunks     int      `json:"total_chunks"`
	CompletedChunks int      `json:"completed_chunks"`
	State           string   `json:"state"`
	RemoteEndpoint  string   `json:"remote_endpoint"`
	Bitfield        []bool   `json:"bitfield"`
	SpeedMbps       float64  `json:"speed_mbps"`
	ETASeconds      int64    `json:"eta_seconds"`
}

// DownloadRequest represents request body for POST /api/download.
type DownloadRequest struct {
	FileID    string `json:"file_id"`
	Peer      string `json:"peer"`
	OutputDir string `json:"output_dir"`
}

// TelemetryEvent represents an event payload sent over SSE (/api/events).
type TelemetryEvent struct {
	Type            string      `json:"type"`
	Timestamp       int64       `json:"timestamp"`
	Payload         interface{} `json:"payload,omitempty"`
	FileID          string      `json:"file_id,omitempty"`
	Index           int         `json:"index,omitempty"`
	TotalChunks     int         `json:"total_chunks,omitempty"`
	CompletedChunks int         `json:"completed_chunks,omitempty"`
	SpeedMbps       float64     `json:"speed_mbps,omitempty"`
	SessionID       string      `json:"session_id,omitempty"`
	State           string      `json:"state,omitempty"`
	TargetPath      string      `json:"target_path,omitempty"`
	PeerID          string      `json:"peer_id,omitempty"`
	Endpoint        string      `json:"endpoint,omitempty"`
	Status          string      `json:"status,omitempty"`
	LatencyMS       int64       `json:"latency_ms,omitempty"`
}
