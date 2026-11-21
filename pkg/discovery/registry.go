package discovery

import (
	"sync"
	"time"
)

// DiscoverySource identifies how a peer was discovered.
type DiscoverySource string

const (
	DiscoveryMDNS   DiscoverySource = "mDNS"
	DiscoveryManual DiscoverySource = "Manual"
)

// PeerStatus indicates connection health.
type PeerStatus string

const (
	PeerStatusConnected    PeerStatus = "Connected"
	PeerStatusDisconnected PeerStatus = "Disconnected"
)

// PeerRecord represents a known remote node in the peer table.
type PeerRecord struct {
	PeerID      string          `json:"peer_id"`
	Endpoint    string          `json:"endpoint"`
	Source      DiscoverySource `json:"source"`
	Status      PeerStatus      `json:"status"`
	LatencyMs   int64           `json:"latency_ms"`
	LastSeen    time.Time       `json:"last_seen"`
	FailedPings int             `json:"failed_pings"`
}

// PeerRegistry maintains the active peer table.
type PeerRegistry struct {
	mu    sync.RWMutex
	peers map[string]*PeerRecord
}

// NewPeerRegistry initializes an empty peer registry.
func NewPeerRegistry() *PeerRegistry {
	return &PeerRegistry{
		peers: make(map[string]*PeerRecord),
	}
}

// RegisterPeer adds or updates a peer in the registry.
func (r *PeerRegistry) RegisterPeer(peerID, endpoint string, source DiscoverySource) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, exists := r.peers[peerID]
	if !exists {
		r.peers[peerID] = &PeerRecord{
			PeerID:      peerID,
			Endpoint:    endpoint,
			Source:      source,
			Status:      PeerStatusConnected,
			LastSeen:    time.Now().UTC(),
			FailedPings: 0,
		}
		return
	}

	rec.Endpoint = endpoint
	rec.Status = PeerStatusConnected
	rec.LastSeen = time.Now().UTC()
}

// GetPeer retrieves a peer record by its PeerID.
func (r *PeerRegistry) GetPeer(peerID string) (*PeerRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rec, exists := r.peers[peerID]
	if !exists {
		return nil, false
	}
	copyRec := *rec
	return &copyRec, true
}

// UpdateLatency sets the round-trip latency for a peer.
func (r *PeerRegistry) UpdateLatency(peerID string, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if rec, exists := r.peers[peerID]; exists {
		rec.LatencyMs = latency.Milliseconds()
		rec.LastSeen = time.Now().UTC()
	}
}

// RecordHeartbeatSuccess resets consecutive failure counts.
func (r *PeerRegistry) RecordHeartbeatSuccess(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if rec, exists := r.peers[peerID]; exists {
		rec.FailedPings = 0
		rec.Status = PeerStatusConnected
		rec.LastSeen = time.Now().UTC()
	}
}

// RecordHeartbeatFailure records a missed ping and evicts the peer if 3 consecutive failures occur.
func (r *PeerRegistry) RecordHeartbeatFailure(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, exists := r.peers[peerID]
	if !exists {
		return
	}

	rec.FailedPings++
	rec.Status = PeerStatusDisconnected

	if rec.FailedPings >= 3 {
		delete(r.peers, peerID)
	}
}

// RemovePeer manually removes a peer from the table.
func (r *PeerRegistry) RemovePeer(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, peerID)
}

// ListPeers returns a slice of all currently known peers.
func (r *PeerRegistry) ListPeers() []*PeerRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()

	records := make([]*PeerRecord, 0, len(r.peers))
	for _, rec := range r.peers {
		copyRec := *rec
		records = append(records, &copyRec)
	}
	return records
}
