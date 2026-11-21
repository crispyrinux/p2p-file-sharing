package discovery

import (
	"testing"
	"time"
)

func TestPeerRegistry_RegisterAndEvict(t *testing.T) {
	reg := NewPeerRegistry()

	peerA := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	endpointA := "192.168.1.50:4001"

	// 1. Register peer
	reg.RegisterPeer(peerA, endpointA, DiscoveryMDNS)
	rec, found := reg.GetPeer(peerA)
	if !found {
		t.Fatalf("expected peer %s to be registered", peerA)
	}
	if rec.Endpoint != endpointA || rec.Source != DiscoveryMDNS {
		t.Errorf("unexpected record content: %+v", rec)
	}
	if rec.Status != PeerStatusConnected {
		t.Errorf("expected status Connected, got %s", rec.Status)
	}

	// 2. Update latency
	reg.UpdateLatency(peerA, 15*time.Millisecond)
	rec, _ = reg.GetPeer(peerA)
	if rec.LatencyMs != 15 {
		t.Errorf("expected latency 15ms, got %dms", rec.LatencyMs)
	}

	// 3. Heartbeat failures & eviction on 3 consecutive drops
	reg.RecordHeartbeatFailure(peerA)
	rec, found = reg.GetPeer(peerA)
	if !found || rec.FailedPings != 1 {
		t.Errorf("expected 1 failed ping, got %+v", rec)
	}

	// Reset on success
	reg.RecordHeartbeatSuccess(peerA)
	rec, _ = reg.GetPeer(peerA)
	if rec.FailedPings != 0 {
		t.Errorf("expected failed pings reset to 0, got %d", rec.FailedPings)
	}

	// 3 consecutive failures trigger eviction
	reg.RecordHeartbeatFailure(peerA)
	reg.RecordHeartbeatFailure(peerA)
	reg.RecordHeartbeatFailure(peerA)

	_, found = reg.GetPeer(peerA)
	if found {
		t.Fatalf("expected peer to be evicted after 3 consecutive heartbeat failures")
	}

	peers := reg.ListPeers()
	if len(peers) != 0 {
		t.Errorf("expected 0 peers in registry, got %d", len(peers))
	}
}

func TestServiceTXT_EncodingDecoding(t *testing.T) {
	peerID := "a1b2c3d4e5f67890123456789abcdef0123456789abcdef0123456789abcdef0"
	version := "1.0.0"

	txtRecords := FormatServiceTXT(peerID, version)
	parsedPeerID, parsedVersion, err := ParseServiceTXT(txtRecords)
	if err != nil {
		t.Fatalf("failed to parse TXT records: %v", err)
	}

	if parsedPeerID != peerID {
		t.Errorf("peerID mismatch: expected %s, got %s", peerID, parsedPeerID)
	}
	if parsedVersion != version {
		t.Errorf("version mismatch: expected %s, got %s", version, parsedVersion)
	}

	// Missing peer_id error
	_, _, err = ParseServiceTXT([]string{"version=1.0.0"})
	if err == nil {
		t.Errorf("expected error with missing peer_id, got nil")
	}

	// Missing version fallback
	parsedPeerID, parsedVersion, err = ParseServiceTXT([]string{"peer_id=" + peerID})
	if err != nil || parsedVersion != ProtocolVer {
		t.Errorf("expected default version %s, got %s (err: %v)", ProtocolVer, parsedVersion, err)
	}

	// RemovePeer test
	reg := NewPeerRegistry()
	reg.RegisterPeer(peerID, "127.0.0.1:4001", DiscoveryManual)
	reg.RemovePeer(peerID)
	if _, found := reg.GetPeer(peerID); found {
		t.Errorf("expected peer to be removed, but still found")
	}

	// MDNSService nil shutdown
	var nilSvc *MDNSService
	nilSvc.Shutdown()
}
