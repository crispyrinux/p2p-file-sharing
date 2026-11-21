package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Opcode identifies the message type on the wire.
type Opcode byte

const (
	OpPing             Opcode = 0x01
	OpPong             Opcode = 0x02
	OpManifestRequest  Opcode = 0x10
	OpManifestResponse Opcode = 0x11
	OpChunkRequest     Opcode = 0x20
	OpChunkResponse    Opcode = 0x21
	OpErrorMessage     Opcode = 0x30
)

// Manifest response status codes.
const (
	ManifestStatusSuccess    byte = 0x00
	ManifestStatusNotFound   byte = 0x01
	ManifestStatusIncomplete byte = 0x02
)

// Protocol error codes.
const (
	ErrCodeInvalidFrame       uint32 = 1001
	ErrCodeUnknownMessage     uint32 = 1002
	ErrCodeManifestValidation uint32 = 2001
	ErrCodeFileNotFound       uint32 = 2002
	ErrCodeChunkOutOfRange    uint32 = 2003
	ErrCodeAuthFailed         uint32 = 3001
	ErrCodeProviderBusy       uint32 = 4001
)

// PingMessage represents a heartbeat probe.
type PingMessage struct {
	Timestamp int64
}

func (m *PingMessage) Encode() []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(m.Timestamp))
	return buf
}

func DecodePing(data []byte) (*PingMessage, error) {
	if len(data) < 8 {
		return nil, errors.New("ping payload too short")
	}
	ts := int64(binary.BigEndian.Uint64(data[:8]))
	return &PingMessage{Timestamp: ts}, nil
}

// ManifestRequestMessage requests file metadata by 32-byte FileID.
type ManifestRequestMessage struct {
	FileID [32]byte
}

func (m *ManifestRequestMessage) Encode() []byte {
	buf := make([]byte, 32)
	copy(buf, m.FileID[:])
	return buf
}

func DecodeManifestRequest(data []byte) (*ManifestRequestMessage, error) {
	if len(data) < 32 {
		return nil, errors.New("manifest request payload must be at least 32 bytes")
	}
	var fileID [32]byte
	copy(fileID[:], data[:32])
	return &ManifestRequestMessage{FileID: fileID}, nil
}

// ManifestResponseMessage delivers the file manifest.
type ManifestResponseMessage struct {
	Status   byte
	Manifest []byte // UTF-8 JSON bytes
}

func (m *ManifestResponseMessage) Encode() []byte {
	jsonLen := uint32(len(m.Manifest))
	buf := make([]byte, 1+4+jsonLen)
	buf[0] = m.Status
	binary.BigEndian.PutUint32(buf[1:5], jsonLen)
	copy(buf[5:], m.Manifest)
	return buf
}

func DecodeManifestResponse(data []byte) (*ManifestResponseMessage, error) {
	if len(data) < 5 {
		return nil, errors.New("manifest response payload too short")
	}
	status := data[0]
	jsonLen := binary.BigEndian.Uint32(data[1:5])
	if uint32(len(data[5:])) < jsonLen {
		return nil, fmt.Errorf("manifest response truncated: expected %d bytes, got %d", jsonLen, len(data[5:]))
	}
	manifest := make([]byte, jsonLen)
	copy(manifest, data[5:5+jsonLen])
	return &ManifestResponseMessage{
		Status:   status,
		Manifest: manifest,
	}, nil
}

// ChunkRequestMessage requests a specific chunk by index.
type ChunkRequestMessage struct {
	FileID [32]byte
	Index  uint32
}

func (m *ChunkRequestMessage) Encode() []byte {
	buf := make([]byte, 32+4)
	copy(buf[:32], m.FileID[:])
	binary.BigEndian.PutUint32(buf[32:36], m.Index)
	return buf
}

func DecodeChunkRequest(data []byte) (*ChunkRequestMessage, error) {
	if len(data) < 36 {
		return nil, errors.New("chunk request payload too short")
	}
	var fileID [32]byte
	copy(fileID[:], data[:32])
	index := binary.BigEndian.Uint32(data[32:36])
	return &ChunkRequestMessage{
		FileID: fileID,
		Index:  index,
	}, nil
}

// ChunkResponseMessage delivers binary chunk payload with its index and checksum.
type ChunkResponseMessage struct {
	FileID   [32]byte
	Index    uint32
	Checksum [32]byte
	Data     []byte
}

func (m *ChunkResponseMessage) Encode() []byte {
	dataLen := uint32(len(m.Data))
	buf := make([]byte, 32+4+32+4+dataLen)
	copy(buf[0:32], m.FileID[:])
	binary.BigEndian.PutUint32(buf[32:36], m.Index)
	copy(buf[36:68], m.Checksum[:])
	binary.BigEndian.PutUint32(buf[68:72], dataLen)
	copy(buf[72:], m.Data)
	return buf
}

func DecodeChunkResponse(data []byte) (*ChunkResponseMessage, error) {
	if len(data) < 72 {
		return nil, errors.New("chunk response payload header too short")
	}
	var fileID [32]byte
	copy(fileID[:], data[0:32])
	index := binary.BigEndian.Uint32(data[32:36])
	var checksum [32]byte
	copy(checksum[:], data[36:68])
	dataLen := binary.BigEndian.Uint32(data[68:72])

	if uint32(len(data[72:])) < dataLen {
		return nil, fmt.Errorf("chunk payload truncated: expected %d bytes, got %d", dataLen, len(data[72:]))
	}

	payload := make([]byte, dataLen)
	copy(payload, data[72:72+dataLen])

	return &ChunkResponseMessage{
		FileID:   fileID,
		Index:    index,
		Checksum: checksum,
		Data:     payload,
	}, nil
}

// ErrorMessage conveys an error code and description.
type ErrorMessage struct {
	Code    uint32
	Message string
}

func (m *ErrorMessage) Encode() []byte {
	msgBytes := []byte(m.Message)
	msgLen := uint16(len(msgBytes))
	buf := make([]byte, 4+2+msgLen)
	binary.BigEndian.PutUint32(buf[0:4], m.Code)
	binary.BigEndian.PutUint16(buf[4:6], msgLen)
	copy(buf[6:], msgBytes)
	return buf
}

func DecodeError(data []byte) (*ErrorMessage, error) {
	if len(data) < 6 {
		return nil, errors.New("error payload too short")
	}
	code := binary.BigEndian.Uint32(data[0:4])
	msgLen := binary.BigEndian.Uint16(data[4:6])
	if len(data[6:]) < int(msgLen) {
		return nil, errors.New("error message string truncated")
	}
	msg := string(data[6 : 6+msgLen])
	return &ErrorMessage{
		Code:    code,
		Message: msg,
	}, nil
}
