package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	"p2p-file-sharing/pkg/identity"
)

// GenerateTLSCertificate creates a self-signed X.509 certificate using the node's Ed25519 identity key.
func GenerateTLSCertificate(id *identity.PeerIdentity) (tls.Certificate, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: id.PeerID,
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, id.PublicKey, id.PrivateKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  id.PrivateKey,
	}
	return cert, nil
}

// ExtractPeerIDFromCert parses an X.509 certificate and extracts the 64-char hex PeerID from its Ed25519 public key.
func ExtractPeerIDFromCert(rawCert []byte) (string, error) {
	cert, err := x509.ParseCertificate(rawCert)
	if err != nil {
		return "", err
	}

	pubKey, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return "", errors.New("certificate public key is not Ed25519")
	}

	return hex.EncodeToString(pubKey), nil
}

// NewServerTLSConfig configures TLS 1.3 server with mutual authentication.
func NewServerTLSConfig(id *identity.PeerIdentity) (*tls.Config, error) {
	cert, err := GenerateTLSCertificate(id)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("no client certificate provided")
			}
			_, err := ExtractPeerIDFromCert(rawCerts[0])
			return err
		},
	}, nil
}

// NewClientTLSConfig configures TLS 1.3 client with mutual authentication and verifies the expected PeerID.
func NewClientTLSConfig(id *identity.PeerIdentity, expectedPeerID string) (*tls.Config, error) {
	cert, err := GenerateTLSCertificate(id)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, // Self-signed certs verified via custom callback below
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errors.New("no server certificate provided")
			}
			peerID, err := ExtractPeerIDFromCert(rawCerts[0])
			if err != nil {
				return err
			}
			if expectedPeerID != "" && peerID != expectedPeerID {
				return fmt.Errorf("peer ID mismatch: expected %s, got %s", expectedPeerID, peerID)
			}
			return nil
		},
	}, nil
}

// UpgradeToTLS runs a client or server TLS 1.3 handshake over an existing net.Conn (e.g. net.Pipe or TCP).
func UpgradeToTLS(rawConn net.Conn, config *tls.Config, isServer bool) (net.Conn, error) {
	var tlsConn *tls.Conn
	if isServer {
		tlsConn = tls.Server(rawConn, config)
	} else {
		tlsConn = tls.Client(rawConn, config)
	}

	if err := tlsConn.Handshake(); err != nil {
		_ = tlsConn.Close()
		_ = rawConn.Close()
		return nil, err
	}
	return tlsConn, nil
}

// ListenTLS binds a TCP listener wrapped with TLS 1.3 server config.
func ListenTLS(addr string, config *tls.Config) (net.Listener, error) {
	return tls.Listen("tcp", addr, config)
}

// DialTLS connects to a remote TCP endpoint with TLS 1.3 mutual authentication.
func DialTLS(addr string, config *tls.Config, timeout time.Duration) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: timeout}
	return tls.DialWithDialer(dialer, "tcp", addr, config)
}
