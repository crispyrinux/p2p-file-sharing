package transfer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogger_JSONAndText(t *testing.T) {
	// 1. JSON logging
	var jsonBuf bytes.Buffer
	jsonLogger := NewLogger(&jsonBuf, true)
	jsonLogger.Log(LevelInfo, "peer_connected", "file123", "peer456", map[string]interface{}{"duration_ms": 42})

	var entry DiagnosticEvent
	if err := json.Unmarshal(jsonBuf.Bytes(), &entry); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}
	if entry.Level != LevelInfo || entry.Event != "peer_connected" || entry.FileID != "file123" {
		t.Errorf("unexpected log entry: %+v", entry)
	}

	// 2. Text logging
	var textBuf bytes.Buffer
	textLogger := NewLogger(&textBuf, false)
	textLogger.Log(LevelError, "verification_failed", "file123", "peer456", map[string]interface{}{"chunk": 2})

	textStr := textBuf.String()
	if !strings.Contains(textStr, "ERROR") || !strings.Contains(textStr, "verification_failed") {
		t.Errorf("unexpected text log: %s", textStr)
	}
}
