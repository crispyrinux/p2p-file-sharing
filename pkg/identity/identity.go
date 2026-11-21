package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// PeerIdentity represents the persistent identity of a P2P node.
type PeerIdentity struct {
	PeerID     string             `json:"peer_id"`
	PublicKey  ed25519.PublicKey  `json:"public_key"`
	PrivateKey ed25519.PrivateKey `json:"private_key"`
	CreatedAt  time.Time          `json:"created_at"`
}

// GenerateIdentity creates a new random Ed25519 keypair and derives the PeerID.
func GenerateIdentity() (*PeerIdentity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	return &PeerIdentity{
		PeerID:     hex.EncodeToString(pub),
		PublicKey:  pub,
		PrivateKey: priv,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

// SaveToFile serializes the identity to a file with restricted 0600 permissions.
func (id *PeerIdentity) SaveToFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// LoadFromFile deserializes a PeerIdentity from disk.
func LoadFromFile(path string) (*PeerIdentity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var id PeerIdentity
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, err
	}

	if len(id.PublicKey) != ed25519.PublicKeySize || len(id.PrivateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid key size in persisted identity")
	}

	computedID := hex.EncodeToString(id.PublicKey)
	if id.PeerID != computedID {
		return nil, errors.New("peer ID mismatch with public key")
	}

	return &id, nil
}

// LoadOrInit loads an existing identity from path or creates and saves a new one if missing.
// Returns the identity, a boolean indicating if it was newly created, and any error encountered.
func LoadOrInit(path string) (*PeerIdentity, bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		id, err := GenerateIdentity()
		if err != nil {
			return nil, false, err
		}
		if err := id.SaveToFile(path); err != nil {
			return nil, false, err
		}
		return id, true, nil
	}

	id, err := LoadFromFile(path)
	if err != nil {
		return nil, false, err
	}
	return id, false, nil
}
