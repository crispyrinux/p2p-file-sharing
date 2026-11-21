package protocol_test

import (
	"bytes"
	"testing"
	"time"

	"p2p-file-sharing/pkg/protocol"
)

func TestFrameReadWrite(t *testing.T) {
	buf := new(bytes.Buffer)
	payload := []byte("hello, decentralized world")

	if err := protocol.WriteFrame(buf, protocol.OpPing, payload); err != nil {
		t.Fatalf("WriteFrame failed: %v", err)
	}

	op, data, err := protocol.ReadFrame(buf)
	if err != nil {
		t.Fatalf("ReadFrame failed: %v", err)
	}

	if op != protocol.OpPing {
		t.Errorf("expected OpPing (0x01), got 0x%02x", op)
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("payload mismatch: expected %q, got %q", payload, data)
	}
}

func TestFrameMaxLimitExceeded(t *testing.T) {
	buf := new(bytes.Buffer)
	hugePayload := make([]byte, protocol.MaxFrameSize+10)

	err := protocol.WriteFrame(buf, protocol.OpChunkResponse, hugePayload)
	if err == nil {
		t.Errorf("expected error for payload exceeding MaxFrameSize, got nil")
	}
}

func TestPingPongEncoding(t *testing.T) {
	now := time.Now().UnixNano()
	ping := &protocol.PingMessage{Timestamp: now}

	data := ping.Encode()
	decoded, err := protocol.DecodePing(data)
	if err != nil {
		t.Fatalf("DecodePing failed: %v", err)
	}
	if decoded.Timestamp != now {
		t.Errorf("timestamp mismatch: %d != %d", decoded.Timestamp, now)
	}
}

func TestManifestRequestResponseEncoding(t *testing.T) {
	var fileID [32]byte
	copy(fileID[:], "0123456789abcdef0123456789abcdef")

	req := &protocol.ManifestRequestMessage{FileID: fileID}
	reqData := req.Encode()

	decodedReq, err := protocol.DecodeManifestRequest(reqData)
	if err != nil {
		t.Fatalf("DecodeManifestRequest failed: %v", err)
	}
	if decodedReq.FileID != fileID {
		t.Errorf("FileID mismatch in ManifestRequest")
	}

	resp := &protocol.ManifestResponseMessage{
		Status:   protocol.ManifestStatusSuccess,
		Manifest: []byte(`{"file_id":"test","total_bytes":1000}`),
	}
	respData := resp.Encode()

	decodedResp, err := protocol.DecodeManifestResponse(respData)
	if err != nil {
		t.Fatalf("DecodeManifestResponse failed: %v", err)
	}
	if decodedResp.Status != protocol.ManifestStatusSuccess {
		t.Errorf("expected status success, got %d", decodedResp.Status)
	}
	if string(decodedResp.Manifest) != string(resp.Manifest) {
		t.Errorf("manifest payload mismatch")
	}
}

func TestChunkRequestResponseEncoding(t *testing.T) {
	var fileID [32]byte
	copy(fileID[:], "0123456789abcdef0123456789abcdef")
	var checksum [32]byte
	copy(checksum[:], "abcdef0123456789abcdef0123456789")

	chunkReq := &protocol.ChunkRequestMessage{
		FileID: fileID,
		Index:  42,
	}
	reqData := chunkReq.Encode()

	decodedReq, err := protocol.DecodeChunkRequest(reqData)
	if err != nil {
		t.Fatalf("DecodeChunkRequest failed: %v", err)
	}
	if decodedReq.FileID != fileID || decodedReq.Index != 42 {
		t.Errorf("ChunkRequest fields mismatch: %+v", decodedReq)
	}

	payload := []byte("chunk binary raw payload bytes here")
	chunkResp := &protocol.ChunkResponseMessage{
		FileID:   fileID,
		Index:    42,
		Checksum: checksum,
		Data:     payload,
	}
	respData := chunkResp.Encode()

	decodedResp, err := protocol.DecodeChunkResponse(respData)
	if err != nil {
		t.Fatalf("DecodeChunkResponse failed: %v", err)
	}
	if decodedResp.FileID != fileID || decodedResp.Index != 42 || decodedResp.Checksum != checksum {
		t.Errorf("ChunkResponse header fields mismatch: %+v", decodedResp)
	}
	if !bytes.Equal(decodedResp.Data, payload) {
		t.Errorf("ChunkResponse payload bytes mismatch")
	}
}

func TestErrorMessageEncoding(t *testing.T) {
	errMsg := &protocol.ErrorMessage{
		Code:    protocol.ErrCodeManifestValidation,
		Message: "manifest chunk count invalid",
	}

	data := errMsg.Encode()
	decoded, err := protocol.DecodeError(data)
	if err != nil {
		t.Fatalf("DecodeError failed: %v", err)
	}
	if decoded.Code != protocol.ErrCodeManifestValidation {
		t.Errorf("expected error code %d, got %d", protocol.ErrCodeManifestValidation, decoded.Code)
	}
	if decoded.Message != errMsg.Message {
		t.Errorf("expected error message %q, got %q", errMsg.Message, decoded.Message)
	}
}
