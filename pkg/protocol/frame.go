package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxFrameSize defines the maximum permissible frame size (4 MB) to prevent buffer exhaustion.
const MaxFrameSize uint32 = 4 * 1024 * 1024

// Common errors for frame operations.
var (
	ErrFrameTooLarge   = errors.New("frame payload exceeds maximum size")
	ErrFrameZeroLength = errors.New("frame payload length cannot be zero")
)

// WriteFrame writes a length-prefixed frame to the provided writer.
// Frame format: [4-byte Payload Length (uint32 big-endian)][1-byte Opcode][Payload Data]
func WriteFrame(w io.Writer, op Opcode, payload []byte) error {
	payloadLen := uint32(1 + len(payload))
	if payloadLen > MaxFrameSize {
		return fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, payloadLen, MaxFrameSize)
	}

	var header [5]byte
	binary.BigEndian.PutUint32(header[0:4], payloadLen)
	header[4] = byte(op)

	if _, err := w.Write(header[:]); err != nil {
		return err
	}

	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}

	return nil
}

// ReadFrame reads a single length-prefixed frame from the reader.
// Returns the opcode, payload data, or an error.
func ReadFrame(r io.Reader) (Opcode, []byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return 0, nil, err
	}

	payloadLen := binary.BigEndian.Uint32(lenBuf[:])
	if payloadLen == 0 {
		return 0, nil, ErrFrameZeroLength
	}
	if payloadLen > MaxFrameSize {
		return 0, nil, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, payloadLen, MaxFrameSize)
	}

	var opBuf [1]byte
	if _, err := io.ReadFull(r, opBuf[:]); err != nil {
		return 0, nil, err
	}
	op := Opcode(opBuf[0])

	dataLen := payloadLen - 1
	if dataLen == 0 {
		return op, nil, nil
	}

	data := make([]byte, dataLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, err
	}

	return op, data, nil
}
