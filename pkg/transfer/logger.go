package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// LogLevel indicates severity.
type LogLevel string

const (
	LevelInfo  LogLevel = "INFO"
	LevelWarn  LogLevel = "WARN"
	LevelError LogLevel = "ERROR"
)

// DiagnosticEvent represents a structured observability record.
type DiagnosticEvent struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     LogLevel               `json:"level"`
	Event     string                 `json:"event"`
	FileID    string                 `json:"file_id,omitempty"`
	PeerID    string                 `json:"peer_id,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// Logger handles structured diagnostic event output.
type Logger struct {
	mu     sync.Mutex
	out    io.Writer
	isJSON bool
}

// NewLogger creates a Logger writing to the given writer.
func NewLogger(out io.Writer, isJSON bool) *Logger {
	if out == nil {
		out = os.Stderr
	}
	return &Logger{
		out:    out,
		isJSON: isJSON,
	}
}

// Log emits a diagnostic event.
func (l *Logger) Log(level LogLevel, event string, fileID, peerID string, details map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry := DiagnosticEvent{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Event:     event,
		FileID:    fileID,
		PeerID:    peerID,
		Details:   details,
	}

	if l.isJSON {
		data, _ := json.Marshal(entry)
		fmt.Fprintln(l.out, string(data))
	} else {
		msg := fmt.Sprintf("[%s] [%s] %s", entry.Timestamp.Format("15:04:05"), level, event)
		if fileID != "" {
			msg += fmt.Sprintf(" file_id=%s", fileID)
		}
		if peerID != "" {
			msg += fmt.Sprintf(" peer_id=%s", peerID)
		}
		if len(details) > 0 {
			msg += fmt.Sprintf(" %v", details)
		}
		fmt.Fprintln(l.out, msg)
	}
}
