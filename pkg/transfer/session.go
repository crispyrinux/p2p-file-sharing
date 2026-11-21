package transfer

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionManager manages persistent transfer session records in .p2p/state/<FileID>.json.
type SessionManager struct {
	stateDir string
	mu       sync.RWMutex
}

// NewSessionManager creates a SessionManager in the specified data directory.
func NewSessionManager(dataDir string) *SessionManager {
	return &SessionManager{
		stateDir: filepath.Join(dataDir, "state"),
	}
}

func (sm *SessionManager) sessionFilePath(fileID string) string {
	return filepath.Join(sm.stateDir, fileID+".json")
}

// CreateOrLoadSession retrieves an existing session or initializes a new persistent session.
func (sm *SessionManager) CreateOrLoadSession(
	fileID, fileName string,
	totalBytes uint64,
	chunkSize, totalChunks uint32,
	peerID, endpoint, targetPath string,
) (*TransferSession, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	path := sm.sessionFilePath(fileID)
	if data, err := os.ReadFile(path); err == nil {
		var sess TransferSession
		if err := json.Unmarshal(data, &sess); err == nil {
			return &sess, nil
		}
	}

	randomBytes := make([]byte, 16)
	_, _ = rand.Read(randomBytes)
	sessionID := hex.EncodeToString(randomBytes)

	sess := &TransferSession{
		SessionID:       sessionID,
		FileID:          fileID,
		FileName:        fileName,
		TotalBytes:      totalBytes,
		ChunkSize:       chunkSize,
		TotalChunks:     totalChunks,
		CompletedChunks: 0,
		State:           StateActive,
		TargetPath:      targetPath,
		RemotePeerID:    peerID,
		RemoteEndpoint:  endpoint,
		Bitfield:        make([]bool, totalChunks),
		StartedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	if err := sm.saveSessionLocked(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// LoadSession loads a session record from disk.
func (sm *SessionManager) LoadSession(fileID string) (*TransferSession, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	data, err := os.ReadFile(sm.sessionFilePath(fileID))
	if err != nil {
		return nil, err
	}

	var sess TransferSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// MarkChunkCompleted updates the bitfield for the specified chunk and saves the record.
func (sm *SessionManager) MarkChunkCompleted(fileID string, index uint32) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sess, err := sm.loadSessionLocked(fileID)
	if err != nil {
		return err
	}

	if index >= uint32(len(sess.Bitfield)) {
		return fmt.Errorf("chunk index %d out of bounds (total %d)", index, len(sess.Bitfield))
	}

	if !sess.Bitfield[index] {
		sess.Bitfield[index] = true
		sess.CompletedChunks++
	}
	sess.UpdatedAt = time.Now().UTC()

	return sm.saveSessionLocked(sess)
}

// GetMissingChunks returns all chunk indices that are not yet verified.
func (sm *SessionManager) GetMissingChunks(fileID string) ([]uint32, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sess, err := sm.loadSessionLocked(fileID)
	if err != nil {
		return nil, err
	}

	var missing []uint32
	for i, completed := range sess.Bitfield {
		if !completed {
			missing = append(missing, uint32(i))
		}
	}
	return missing, nil
}

// UpdateState sets the session state and persists to disk.
func (sm *SessionManager) UpdateState(fileID string, state SessionState) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sess, err := sm.loadSessionLocked(fileID)
	if err != nil {
		return err
	}

	sess.State = state
	sess.UpdatedAt = time.Now().UTC()
	return sm.saveSessionLocked(sess)
}

// ListSessions returns all persisted transfer sessions.
func (sm *SessionManager) ListSessions() ([]*TransferSession, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if err := os.MkdirAll(sm.stateDir, 0755); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(sm.stateDir)
	if err != nil {
		return nil, err
	}

	var sessions []*TransferSession
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sm.stateDir, entry.Name()))
		if err != nil {
			continue
		}
		var sess TransferSession
		if err := json.Unmarshal(data, &sess); err == nil {
			sessions = append(sessions, &sess)
		}
	}
	return sessions, nil
}

// CleanSession removes the persistent session file after completion or failure.
func (sm *SessionManager) CleanSession(fileID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return os.Remove(sm.sessionFilePath(fileID))
}

func (sm *SessionManager) loadSessionLocked(fileID string) (*TransferSession, error) {
	data, err := os.ReadFile(sm.sessionFilePath(fileID))
	if err != nil {
		return nil, err
	}
	var sess TransferSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (sm *SessionManager) saveSessionLocked(sess *TransferSession) error {
	if err := os.MkdirAll(sm.stateDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sm.sessionFilePath(sess.FileID), data, 0644)
}
