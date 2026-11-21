package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"p2p-file-sharing/pkg/manifest"
	"p2p-file-sharing/pkg/protocol"
)

// SessionState tracks the lifecycle of a transfer session.
type SessionState string

const (
	StateQueued      SessionState = "Queued"
	StateActive      SessionState = "Active"
	StateInterrupted SessionState = "Interrupted"
	StateCompleted   SessionState = "Completed"
	StateFailed      SessionState = "Failed"
)

// PublishedFile represents a locally hosted complete file.
type PublishedFile struct {
	Manifest   *manifest.FileManifest
	SourcePath string
}

// TransferSession tracks an active or paused transfer.
type TransferSession struct {
	SessionID       string       `json:"session_id"`
	FileID          string       `json:"file_id"`
	FileName        string       `json:"file_name"`
	TotalBytes      uint64       `json:"total_bytes"`
	ChunkSize       uint32       `json:"chunk_size"`
	TotalChunks     uint32       `json:"total_chunks"`
	CompletedChunks uint32       `json:"completed_chunks"`
	State           SessionState `json:"state"`
	TargetPath      string       `json:"target_path"`
	RemotePeerID    string       `json:"remote_peer_id"`
	RemoteEndpoint  string       `json:"remote_endpoint"`
	Bitfield        []bool       `json:"bitfield"`
	StartedAt       time.Time    `json:"started_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

// TransferEngine coordinates chunk requests, responses, and session lifecycles.
type TransferEngine struct {
	staging        *StagingManager
	sessionMgr     *SessionManager
	muPublished    sync.RWMutex
	publishedFiles map[string]*PublishedFile
}

// NewTransferEngine initializes a TransferEngine with the provided staging manager.
func NewTransferEngine(staging *StagingManager) *TransferEngine {
	return &TransferEngine{
		staging:        staging,
		sessionMgr:     NewSessionManager(staging.rootDir),
		publishedFiles: make(map[string]*PublishedFile),
	}
}

// SessionManager returns the underlying session manager.
func (e *TransferEngine) SessionManager() *SessionManager {
	return e.sessionMgr
}

// RegisterPublishedFile registers a complete local file for network sharing.
func (e *TransferEngine) RegisterPublishedFile(m *manifest.FileManifest, filePath string) error {
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("published file not accessible: %w", err)
	}
	if uint64(stat.Size()) != m.TotalBytes {
		return fmt.Errorf("file size mismatch: expected %d, got %d", m.TotalBytes, stat.Size())
	}

	e.muPublished.Lock()
	defer e.muPublished.Unlock()
	e.publishedFiles[m.FileID] = &PublishedFile{
		Manifest:   m,
		SourcePath: filePath,
	}
	return nil
}

// GetPublishedFile retrieves a published file record by FileID.
func (e *TransferEngine) GetPublishedFile(fileID string) (*PublishedFile, bool) {
	e.muPublished.RLock()
	defer e.muPublished.RUnlock()
	pub, ok := e.publishedFiles[fileID]
	return pub, ok
}

// HandleConnection processes incoming requests on an established peer connection.
func (e *TransferEngine) HandleConnection(conn net.Conn) error {
	defer conn.Close()

	for {
		op, payload, err := protocol.ReadFrame(conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}

		switch op {
		case protocol.OpPing:
			pingMsg, err := protocol.DecodePing(payload)
			if err != nil {
				continue
			}
			pongMsg := &protocol.PingMessage{Timestamp: pingMsg.Timestamp}
			if err := protocol.WriteFrame(conn, protocol.OpPong, pongMsg.Encode()); err != nil {
				return err
			}

		case protocol.OpManifestRequest:
			req, err := protocol.DecodeManifestRequest(payload)
			if err != nil {
				_ = sendError(conn, protocol.ErrCodeInvalidFrame, "malformed manifest request")
				continue
			}
			fileID := hex.EncodeToString(req.FileID[:])
			pub, found := e.GetPublishedFile(fileID)
			if !found {
				resp := &protocol.ManifestResponseMessage{
					Status:   protocol.ManifestStatusNotFound,
					Manifest: nil,
				}
				_ = protocol.WriteFrame(conn, protocol.OpManifestResponse, resp.Encode())
				continue
			}

			manifestJSON, err := json.Marshal(pub.Manifest)
			if err != nil {
				_ = sendError(conn, protocol.ErrCodeFileNotFound, "failed to serialize manifest")
				continue
			}
			resp := &protocol.ManifestResponseMessage{
				Status:   protocol.ManifestStatusSuccess,
				Manifest: manifestJSON,
			}
			if err := protocol.WriteFrame(conn, protocol.OpManifestResponse, resp.Encode()); err != nil {
				return err
			}

		case protocol.OpChunkRequest:
			req, err := protocol.DecodeChunkRequest(payload)
			if err != nil {
				_ = sendError(conn, protocol.ErrCodeInvalidFrame, "malformed chunk request")
				continue
			}
			fileID := hex.EncodeToString(req.FileID[:])
			pub, found := e.GetPublishedFile(fileID)
			if !found {
				_ = sendError(conn, protocol.ErrCodeFileNotFound, "requested file not found")
				continue
			}

			if req.Index >= pub.Manifest.ChunkCount {
				_ = sendError(conn, protocol.ErrCodeChunkOutOfRange, "chunk index out of bounds")
				continue
			}

			chunkData, err := readChunkFromFile(pub.SourcePath, req.Index, pub.Manifest.ChunkSize, pub.Manifest.TotalBytes)
			if err != nil {
				_ = sendError(conn, protocol.ErrCodeFileNotFound, fmt.Sprintf("failed to read chunk: %v", err))
				continue
			}

			checksum := sha256.Sum256(chunkData)
			var rawFID [32]byte
			copy(rawFID[:], req.FileID[:])

			chunkResp := &protocol.ChunkResponseMessage{
				FileID:   rawFID,
				Index:    req.Index,
				Checksum: checksum,
				Data:     chunkData,
			}
			if err := protocol.WriteFrame(conn, protocol.OpChunkResponse, chunkResp.Encode()); err != nil {
				return err
			}

		case protocol.OpErrorMessage:
			// Peer sent error
			return nil
		}
	}
}

// DownloadFile connects to a peer and downloads a file by its FileID.
func (e *TransferEngine) DownloadFile(
	ctx context.Context,
	conn net.Conn,
	fileID string,
	targetPath string,
	progressCb func(completed, total uint32),
) error {
	rawFID, err := hex.DecodeString(fileID)
	if err != nil || len(rawFID) != 32 {
		return fmt.Errorf("invalid file ID format: %s", fileID)
	}

	var binaryFID [32]byte
	copy(binaryFID[:], rawFID)

	// Step 1: Send ManifestRequest
	manifestReq := &protocol.ManifestRequestMessage{FileID: binaryFID}
	if err := protocol.WriteFrame(conn, protocol.OpManifestRequest, manifestReq.Encode()); err != nil {
		return fmt.Errorf("failed to send manifest request: %w", err)
	}

	// Step 2: Read ManifestResponse
	op, payload, err := protocol.ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("failed to read manifest response: %w", err)
	}
	if op == protocol.OpErrorMessage {
		errMsg, _ := protocol.DecodeError(payload)
		return fmt.Errorf("provider returned error: %s (code %d)", errMsg.Message, errMsg.Code)
	}
	if op != protocol.OpManifestResponse {
		return fmt.Errorf("unexpected opcode received: 0x%02x, expected 0x11", op)
	}

	manifestResp, err := protocol.DecodeManifestResponse(payload)
	if err != nil {
		return fmt.Errorf("failed to decode manifest response: %w", err)
	}
	if manifestResp.Status == protocol.ManifestStatusNotFound {
		return errors.New("file not found on provider")
	}
	if manifestResp.Status != protocol.ManifestStatusSuccess {
		return fmt.Errorf("provider returned manifest status: 0x%02x", manifestResp.Status)
	}

	var m manifest.FileManifest
	if err := json.Unmarshal(manifestResp.Manifest, &m); err != nil {
		return fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	// Step 3: Validate Manifest
	if err := manifest.ValidateManifest(&m, fileID); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}
	if m.FileID != fileID {
		return fmt.Errorf("manifest FileID %s does not match requested %s", m.FileID, fileID)
	}

	// Step 4: Initialize or Load Session
	remoteAddr := ""
	if conn.RemoteAddr() != nil {
		remoteAddr = conn.RemoteAddr().String()
	}
	_, _ = e.sessionMgr.CreateOrLoadSession(m.FileID, m.FileName, m.TotalBytes, m.ChunkSize, m.ChunkCount, "", remoteAddr, targetPath)
	_ = e.sessionMgr.UpdateState(m.FileID, StateActive)

	// Step 5: Download chunks
	var completed uint32
	for i := uint32(0); i < m.ChunkCount; i++ {
		if err := ctx.Err(); err != nil {
			_ = e.sessionMgr.UpdateState(m.FileID, StateInterrupted)
			return err
		}

		if e.staging.HasChunk(m.FileID, i) {
			_ = e.sessionMgr.MarkChunkCompleted(m.FileID, i)
			completed++
			if progressCb != nil {
				progressCb(completed, m.ChunkCount)
			}
			continue
		}

		chunkReq := &protocol.ChunkRequestMessage{
			FileID: binaryFID,
			Index:  i,
		}
		if err := protocol.WriteFrame(conn, protocol.OpChunkRequest, chunkReq.Encode()); err != nil {
			_ = e.sessionMgr.UpdateState(m.FileID, StateInterrupted)
			return fmt.Errorf("failed to send chunk request for index %d: %w", i, err)
		}

		chunkOp, chunkPayload, err := protocol.ReadFrame(conn)
		if err != nil {
			_ = e.sessionMgr.UpdateState(m.FileID, StateInterrupted)
			return fmt.Errorf("failed to read chunk %d response: %w", i, err)
		}
		if chunkOp == protocol.OpErrorMessage {
			_ = e.sessionMgr.UpdateState(m.FileID, StateFailed)
			errMsg, _ := protocol.DecodeError(chunkPayload)
			return fmt.Errorf("provider returned error on chunk %d: %s", i, errMsg.Message)
		}
		if chunkOp != protocol.OpChunkResponse {
			_ = e.sessionMgr.UpdateState(m.FileID, StateFailed)
			return fmt.Errorf("unexpected opcode for chunk %d: 0x%02x", i, chunkOp)
		}

		resp, err := protocol.DecodeChunkResponse(chunkPayload)
		if err != nil {
			_ = e.sessionMgr.UpdateState(m.FileID, StateFailed)
			return fmt.Errorf("failed to decode chunk %d response: %w", i, err)
		}
		if resp.Index != i {
			_ = e.sessionMgr.UpdateState(m.FileID, StateFailed)
			return fmt.Errorf("received chunk index %d, expected %d", resp.Index, i)
		}

		// Verify chunk checksum against manifest
		expectedHash := m.ChunkHashes[i]
		actualSum := sha256.Sum256(resp.Data)
		actualHash := hex.EncodeToString(actualSum[:])
		if actualHash != expectedHash {
			_ = e.sessionMgr.UpdateState(m.FileID, StateFailed)
			return fmt.Errorf("chunk %d hash mismatch: expected %s, got %s", i, expectedHash, actualHash)
		}

		if err := e.staging.SaveChunk(m.FileID, i, resp.Data, expectedHash); err != nil {
			_ = e.sessionMgr.UpdateState(m.FileID, StateInterrupted)
			return fmt.Errorf("failed to save chunk %d to staging: %w", i, err)
		}
		_ = e.sessionMgr.MarkChunkCompleted(m.FileID, i)

		completed++
		if progressCb != nil {
			progressCb(completed, m.ChunkCount)
		}
	}

	// Step 6: Assemble File & Verify whole-file SHA-256
	if err := e.staging.AssembleFile(&m, targetPath); err != nil {
		_ = e.sessionMgr.UpdateState(m.FileID, StateFailed)
		return fmt.Errorf("file assembly failed: %w", err)
	}

	// Step 7: Clean staging & Mark Completed
	_ = e.staging.CleanStaging(m.FileID)
	_ = e.sessionMgr.UpdateState(m.FileID, StateCompleted)

	return nil
}

func sendError(w io.Writer, code uint32, message string) error {
	errMsg := &protocol.ErrorMessage{
		Code:    code,
		Message: message,
	}
	return protocol.WriteFrame(w, protocol.OpErrorMessage, errMsg.Encode())
}

func readChunkFromFile(filePath string, index uint32, chunkSize uint32, totalBytes uint64) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	offset := int64(index) * int64(chunkSize)
	remaining := totalBytes - uint64(offset)
	readLen := chunkSize
	if uint64(readLen) > remaining {
		readLen = uint32(remaining)
	}

	buf := make([]byte, readLen)
	_, err = file.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf, nil
}
