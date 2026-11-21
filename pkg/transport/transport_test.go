package transport_test

import (
	"net"
	"testing"
	"time"

	"p2p-file-sharing/pkg/identity"
	"p2p-file-sharing/pkg/transport"
	"p2p-file-sharing/tests/harness"
)

func TestMutualTLSHandshakeOverPipe(t *testing.T) {
	nodeA, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity A failed: %v", err)
	}
	nodeB, err := identity.GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity B failed: %v", err)
	}

	serverPipe, clientPipe := harness.NewSimulatedPipe()

	serverTLS, err := transport.NewServerTLSConfig(nodeA)
	if err != nil {
		t.Fatalf("NewServerTLSConfig failed: %v", err)
	}

	// Client expects nodeA.PeerID
	clientTLS, err := transport.NewClientTLSConfig(nodeB, nodeA.PeerID)
	if err != nil {
		t.Fatalf("NewClientTLSConfig failed: %v", err)
	}

	errCh := make(chan error, 2)
	var serverConn, clientConn net.Conn

	go func() {
		sConn, err := transport.UpgradeToTLS(serverPipe, serverTLS, true)
		serverConn = sConn
		errCh <- err
	}()

	go func() {
		cConn, err := transport.UpgradeToTLS(clientPipe, clientTLS, false)
		clientConn = cConn
		errCh <- err
	}()

	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("TLS upgrade error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("TLS handshake timed out")
		}
	}
	defer serverConn.Close()
	defer clientConn.Close()

	// Verify bidirectional communication over the encrypted channel
	testMsg := []byte("encrypted p2p test payload")
	go func() {
		_, _ = clientConn.Write(testMsg)
	}()

	buf := make([]byte, len(testMsg))
	n, err := serverConn.Read(buf)
	if err != nil {
		t.Fatalf("server read failed: %v", err)
	}
	if string(buf[:n]) != string(testMsg) {
		t.Errorf("expected %q, got %q", testMsg, buf[:n])
	}
}

func TestMutualTLSRejectsMismatchedPeerID(t *testing.T) {
	nodeA, _ := identity.GenerateIdentity()
	nodeB, _ := identity.GenerateIdentity()
	otherNode, _ := identity.GenerateIdentity()

	serverTLS, _ := transport.NewServerTLSConfig(nodeA)
	ln, err := transport.ListenTLS("127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatalf("ListenTLS failed: %v", err)
	}
	defer ln.Close()

	// Client expects otherNode.PeerID (not nodeA.PeerID) -> should be rejected!
	clientTLS, _ := transport.NewClientTLSConfig(nodeB, otherNode.PeerID)

	serverErrCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErrCh <- err
			return
		}
		defer conn.Close()
		var buf [1]byte
		_, rErr := conn.Read(buf[:])
		serverErrCh <- rErr
	}()

	clientConn, clientErr := transport.DialTLS(ln.Addr().String(), clientTLS, 2*time.Second)
	if clientErr == nil {
		_ = clientConn.Close()
		t.Errorf("expected client DialTLS to fail due to PeerID mismatch, got nil")
	}
}

func TestDialTLS_SuccessRoundTrip(t *testing.T) {
	nodeA, _ := identity.GenerateIdentity()
	nodeB, _ := identity.GenerateIdentity()

	serverTLS, _ := transport.NewServerTLSConfig(nodeA)
	ln, err := transport.ListenTLS("127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatalf("ListenTLS failed: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			_, _ = conn.Write([]byte("pong"))
		}
	}()

	clientTLS, _ := transport.NewClientTLSConfig(nodeB, nodeA.PeerID)
	clientConn, err := transport.DialTLS(ln.Addr().String(), clientTLS, 2*time.Second)
	if err != nil {
		t.Fatalf("DialTLS failed: %v", err)
	}
	defer clientConn.Close()

	var buf [4]byte
	_, err = clientConn.Read(buf[:])
	if err != nil || string(buf[:]) != "pong" {
		t.Fatalf("expected pong response: %v", err)
	}
}

func TestExtractPeerIDFromCert_InvalidCert(t *testing.T) {
	_, err := transport.ExtractPeerIDFromCert([]byte("invalid-cert-bytes"))
	if err == nil {
		t.Errorf("expected error extracting peer ID from invalid cert, got nil")
	}

	id, _ := identity.GenerateIdentity()
	serverTLS, _ := transport.NewServerTLSConfig(id)
	if err := serverTLS.VerifyPeerCertificate(nil, nil); err == nil {
		t.Errorf("expected error for empty rawCerts on server, got nil")
	}

	clientTLS, _ := transport.NewClientTLSConfig(id, id.PeerID)
	if err := clientTLS.VerifyPeerCertificate(nil, nil); err == nil {
		t.Errorf("expected error for empty rawCerts on client, got nil")
	}
}
