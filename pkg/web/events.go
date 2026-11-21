package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// EventHub manages connected Server-Sent Events (SSE) clients and broadcasts telemetry.
type EventHub struct {
	mu      sync.RWMutex
	clients map[chan TelemetryEvent]struct{}
	closed  bool
}

// NewEventHub creates a new thread-safe EventHub.
func NewEventHub() *EventHub {
	return &EventHub{
		clients: make(map[chan TelemetryEvent]struct{}),
	}
}

// Register adds a new client channel to the hub.
func (h *EventHub) Register(ch chan TelemetryEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.clients[ch] = struct{}{}
	}
}

// Unregister removes a client channel from the hub and closes it.
func (h *EventHub) Unregister(ch chan TelemetryEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
}

// Broadcast dispatches a telemetry event to all registered clients.
// Non-blocking write: if a client buffer is full, it drops or continues to avoid stalls.
func (h *EventHub) Broadcast(event TelemetryEvent) {
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return
	}

	for ch := range h.clients {
		select {
		case ch <- event:
		default:
			// Buffer full, drop to prevent slow clients blocking broadcaster
		}
	}
}

// ClientCount returns the number of active SSE listeners.
func (h *EventHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close shuts down the hub and unregisters all clients.
func (h *EventHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.clients {
		close(ch)
	}
	h.clients = make(map[chan TelemetryEvent]struct{})
}

// ServeHTTP implements http.Handler for streaming Server-Sent Events.
func (h *EventHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientChan := make(chan TelemetryEvent, 64)
	h.Register(clientChan)
	defer h.Unregister(clientChan)

	// Send initial connected event
	initial := TelemetryEvent{
		Type:      "connected",
		Timestamp: time.Now().Unix(),
	}
	data, _ := json.Marshal(initial)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-clientChan:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
