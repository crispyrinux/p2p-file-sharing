package transfer

import (
	"path/filepath"
	"testing"
)

func TestSessionManager_PersistenceAndMissingChunks(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, ".p2p")
	sm := NewSessionManager(stateDir)

	fileID := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	peerID := "peer1234567890"

	// 1. Create new session with 5 chunks
	sess, err := sm.CreateOrLoadSession(fileID, "document.pdf", 5*524288, 524288, 5, peerID, "127.0.0.1:4001", "downloads/document.pdf")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	if sess.State != StateActive && sess.State != StateQueued {
		t.Errorf("unexpected initial state: %s", sess.State)
	}
	if len(sess.Bitfield) != 5 {
		t.Fatalf("expected bitfield length 5, got %d", len(sess.Bitfield))
	}

	// 2. Mark chunks 0, 1, 3 completed
	if err := sm.MarkChunkCompleted(fileID, 0); err != nil {
		t.Fatalf("failed to mark chunk 0: %v", err)
	}
	if err := sm.MarkChunkCompleted(fileID, 1); err != nil {
		t.Fatalf("failed to mark chunk 1: %v", err)
	}
	if err := sm.MarkChunkCompleted(fileID, 3); err != nil {
		t.Fatalf("failed to mark chunk 3: %v", err)
	}

	// 3. Check missing chunks
	missing, err := sm.GetMissingChunks(fileID)
	if err != nil {
		t.Fatalf("failed to get missing chunks: %v", err)
	}
	if len(missing) != 2 || missing[0] != 2 || missing[1] != 4 {
		t.Fatalf("expected missing chunks [2, 4], got %v", missing)
	}

	// 4. Update session state to Interrupted
	if err := sm.UpdateState(fileID, StateInterrupted); err != nil {
		t.Fatalf("failed to update state: %v", err)
	}

	// 5. Simulate restart: create a new SessionManager pointing to same directory
	sm2 := NewSessionManager(stateDir)
	loaded, err := sm2.LoadSession(fileID)
	if err != nil {
		t.Fatalf("failed to load persisted session: %v", err)
	}
	if loaded.State != StateInterrupted {
		t.Errorf("expected loaded state Interrupted, got %s", loaded.State)
	}
	if loaded.CompletedChunks != 3 {
		t.Errorf("expected 3 completed chunks, got %d", loaded.CompletedChunks)
	}
	if !loaded.Bitfield[0] || !loaded.Bitfield[1] || loaded.Bitfield[2] || !loaded.Bitfield[3] || loaded.Bitfield[4] {
		t.Errorf("bitfield was not preserved accurately: %v", loaded.Bitfield)
	}

	// 6. Complete remaining chunks
	_ = sm2.MarkChunkCompleted(fileID, 2)
	_ = sm2.MarkChunkCompleted(fileID, 4)
	missingAfter, _ := sm2.GetMissingChunks(fileID)
	if len(missingAfter) != 0 {
		t.Errorf("expected 0 missing chunks after completion, got %v", missingAfter)
	}

	// 7. Clean session
	if err := sm2.CleanSession(fileID); err != nil {
		t.Fatalf("failed to clean session: %v", err)
	}
	_, err = sm2.LoadSession(fileID)
	if err == nil {
		t.Errorf("expected error loading cleaned session, got nil")
	}
}

func TestSessionManager_ListSessions(t *testing.T) {
	tempDir := t.TempDir()
	sm := NewSessionManager(tempDir)

	_, _ = sm.CreateOrLoadSession("file1", "a.txt", 1000, 500, 2, "peer1", "127.0.0.1:4001", "downloads/a.txt")
	_, _ = sm.CreateOrLoadSession("file2", "b.txt", 2000, 500, 4, "peer2", "127.0.0.1:4002", "downloads/b.txt")

	list, err := sm.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(list))
	}
}
