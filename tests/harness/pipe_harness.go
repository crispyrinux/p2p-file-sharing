package harness

import (
	"crypto/rand"
	"errors"
	"net"
	"sync"
	"time"
)

// SimulatedConn wraps a net.Conn with latency, drop rate, and fault injection capabilities.
type SimulatedConn struct {
	net.Conn
	mu           sync.Mutex
	latency      time.Duration
	dropRate     float64 // 0.0 to 1.0 probability of dropping a write
	corruptRate  float64 // 0.0 to 1.0 probability of flipping bits in writes
	closed       bool
}

// NewSimulatedPipe creates an in-memory bidirectional pipe connecting two SimulatedConn endpoints.
func NewSimulatedPipe() (*SimulatedConn, *SimulatedConn) {
	c1, c2 := net.Pipe()
	return &SimulatedConn{Conn: c1}, &SimulatedConn{Conn: c2}
}

// SetLatency sets artificial network latency for writes.
func (c *SimulatedConn) SetLatency(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latency = d
}

// SetDropRate sets the probability of dropping writes (0.0 = none, 1.0 = drop all).
func (c *SimulatedConn) SetDropRate(rate float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropRate = rate
}

// SetCorruptRate sets the probability of corrupting byte payloads during write.
func (c *SimulatedConn) SetCorruptRate(rate float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.corruptRate = rate
}

// Write writes data through the simulated connection with configured faults and latency.
func (c *SimulatedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	closed := c.closed
	latency := c.latency
	dropRate := c.dropRate
	corruptRate := c.corruptRate
	c.mu.Unlock()

	if closed {
		return 0, errors.New("connection closed")
	}

	if latency > 0 {
		time.Sleep(latency)
	}

	if dropRate > 0 {
		var randByte [1]byte
		_, _ = rand.Read(randByte[:])
		if float64(randByte[0])/255.0 < dropRate {
			// Simulate drop: pretend write succeeded but don't deliver
			return len(b), nil
		}
	}

	buf := make([]byte, len(b))
	copy(buf, b)

	if corruptRate > 0 && len(buf) > 0 {
		var randByte [1]byte
		_, _ = rand.Read(randByte[:])
		if float64(randByte[0])/255.0 < corruptRate {
			// Corrupt the first byte by flipping bits
			buf[0] ^= 0xFF
		}
	}

	return c.Conn.Write(buf)
}

// Close terminates the connection.
func (c *SimulatedConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return c.Conn.Close()
}
