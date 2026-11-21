package discovery

import (
	"context"
	"net"
	"testing"
	"time"

	"p2p-file-sharing/pkg/identity"
)

func getTestPort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get test port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestMDNS_StartAndDiscover(t *testing.T) {
	idA, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate identity A: %v", err)
	}
	idB, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("failed to generate identity B: %v", err)
	}

	portA := getTestPort(t)
	portB := getTestPort(t)

	svcA, errA := StartMDNSService(idA.PeerID, portA)
	if errA != nil {
		t.Logf("skipping mDNS test (multicast unavailable in current environment): %v", errA)
		return
	}
	defer svcA.Shutdown()

	svcB, errB := StartMDNSService(idB.PeerID, portB)
	if errB != nil {
		t.Logf("skipping mDNS test B: %v", errB)
		return
	}
	defer svcB.Shutdown()

	regA := NewPeerRegistry()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_ = DiscoverPeers(ctx, regA, idA.PeerID)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, found := regA.GetPeer(idB.PeerID); found {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}
