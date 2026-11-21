package transfer

import (
	"context"
	"errors"
	"net"
	"time"

	"p2p-file-sharing/pkg/protocol"
)

// HeartbeatMonitor manages periodic keep-alive probes and peer liveness detection.
type HeartbeatMonitor struct {
	conn     net.Conn
	interval time.Duration
	timeout  time.Duration
}

// NewHeartbeatMonitor initializes a heartbeat monitor for an established connection.
func NewHeartbeatMonitor(conn net.Conn, interval, timeout time.Duration) *HeartbeatMonitor {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &HeartbeatMonitor{
		conn:     conn,
		interval: interval,
		timeout:  timeout,
	}
}

// PingOnce sends a single Ping frame and awaits the Pong response within the timeout.
func (hm *HeartbeatMonitor) PingOnce(ctx context.Context) error {
	pingMsg := &protocol.PingMessage{
		Timestamp: time.Now().UnixNano(),
	}

	_ = hm.conn.SetWriteDeadline(time.Now().Add(hm.timeout))
	if err := protocol.WriteFrame(hm.conn, protocol.OpPing, pingMsg.Encode()); err != nil {
		return err
	}
	_ = hm.conn.SetWriteDeadline(time.Time{})

	errChan := make(chan error, 1)
	go func() {
		_ = hm.conn.SetReadDeadline(time.Now().Add(hm.timeout))
		op, _, err := protocol.ReadFrame(hm.conn)
		_ = hm.conn.SetReadDeadline(time.Time{})
		if err != nil {
			errChan <- err
			return
		}
		if op != protocol.OpPong {
			errChan <- errors.New("expected Pong response from peer")
			return
		}
		errChan <- nil
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errChan:
		return err
	}
}

// Start begins a background ticker sending heartbeats and triggers onTimeout upon failure.
func (hm *HeartbeatMonitor) Start(ctx context.Context, onTimeout func()) {
	ticker := time.NewTicker(hm.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pingCtx, cancel := context.WithTimeout(ctx, hm.timeout)
				err := hm.PingOnce(pingCtx)
				cancel()
				if err != nil {
					if onTimeout != nil {
						onTimeout()
					}
					return
				}
			}
		}
	}()
}
