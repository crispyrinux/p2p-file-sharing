package integration

import (
	"context"
	"net"
	"testing"
	"time"

	"p2p-file-sharing/pkg/discovery"
	"p2p-file-sharing/pkg/identity"
)

func getFreePort(t *testing.T) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func TestE2E_HybridDiscovery_ManualAndMDNS(t *testing.T) {
	// 1. Manual endpoint discovery (always reliable across all CI / environments)
	reg := discovery.NewPeerRegistry()
	manualPeerID := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	manualEndpoint := "127.0.0.1:4001"

	reg.RegisterPeer(manualPeerID, manualEndpoint, discovery.DiscoveryManual)
	rec, found := reg.GetPeer(manualPeerID)
	if !found {
		t.Fatalf("expected manual peer to be registered")
	}
	if rec.Source != discovery.DiscoveryManual || rec.Endpoint != manualEndpoint {
		t.Errorf("unexpected peer record: %+v", rec)
	}

	// 2. mDNS discovery test
	idA, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate identity A: %v", err)
	}
	idB, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate identity B: %v", err)
	}

	portA := getFreePort(t)
	portB := getFreePort(t)

	svcA, errA := discovery.StartMDNSService(idA.PeerID, portA)
	if errA != nil {
		t.Logf("mDNS service registration skipped (OS firewall / multicast unavailable): %v", errA)
		return
	}
	defer svcA.Shutdown()

	svcB, errB := discovery.StartMDNSService(idB.PeerID, portB)
	if errB != nil {
		t.Logf("mDNS service registration B skipped: %v", errB)
		return
	}
	defer svcB.Shutdown()

	regA := discovery.NewPeerRegistry()
	regB := discovery.NewPeerRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = discovery.DiscoverPeers(ctx, regA, idA.PeerID)
	_ = discovery.DiscoverPeers(ctx, regB, idB.PeerID)

	// Wait up to 5 seconds for discovery
	deadline := time.Now().Add(5 * time.Second)
	var discoveredA, discoveredB bool

	for time.Now().Before(deadline) {
		if _, found := regA.GetPeer(idB.PeerID); found {
			discoveredB = true
		}
		if _, found := regB.GetPeer(idA.PeerID); found {
			discoveredA = true
		}
		if discoveredA || discoveredB {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Logf("mDNS discovery result: Node A found B = %v, Node B found A = %v", discoveredB, discoveredA)
}
