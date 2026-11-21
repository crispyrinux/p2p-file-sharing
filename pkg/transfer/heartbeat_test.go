package transfer

import (
	"context"
	"net"
	"testing"
	"time"

	"p2p-file-sharing/pkg/protocol"
)

func TestHeartbeat_PingPongSuccess(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Server handles ping
	go func() {
		op, payload, err := protocol.ReadFrame(serverConn)
		if err != nil {
			return
		}
		if op == protocol.OpPing {
			ping, _ := protocol.DecodePing(payload)
			pong := &protocol.PingMessage{Timestamp: ping.Timestamp}
			_ = protocol.WriteFrame(serverConn, protocol.OpPong, pong.Encode())
		}
	}()

	hb := NewHeartbeatMonitor(clientConn, 1*time.Second, 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := hb.PingOnce(ctx)
	if err != nil {
		t.Fatalf("expected ping to succeed, got error: %v", err)
	}
}

func TestHeartbeat_TimeoutDetection(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Server does NOT respond to ping (silent)
	hb := NewHeartbeatMonitor(clientConn, 500*time.Millisecond, 1*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := hb.PingOnce(ctx)
	if err == nil {
		t.Fatalf("expected timeout error when peer does not respond to ping, got nil")
	}
}
