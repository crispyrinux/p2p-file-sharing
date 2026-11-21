package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/grandcat/zeroconf"
)

// MDNS Service definitions per FR-002 and research decisions.
const (
	ServiceType   = "_p2pfileshare._tcp"
	ServiceDomain = "local."
	ProtocolVer   = "1.0.0"
)

// FormatServiceTXT formats peer metadata into DNS TXT records.
func FormatServiceTXT(peerID, version string) []string {
	return []string{
		"peer_id=" + peerID,
		"version=" + version,
	}
}

// ParseServiceTXT extracts peerID and version from DNS TXT records.
func ParseServiceTXT(records []string) (string, string, error) {
	var peerID, version string
	for _, rec := range records {
		if strings.HasPrefix(rec, "peer_id=") {
			peerID = strings.TrimPrefix(rec, "peer_id=")
		} else if strings.HasPrefix(rec, "version=") {
			version = strings.TrimPrefix(rec, "version=")
		}
	}

	if peerID == "" {
		return "", "", errors.New("missing peer_id in TXT records")
	}
	if version == "" {
		version = ProtocolVer
	}
	return peerID, version, nil
}

// MDNSService wraps a Zeroconf mDNS advertisement server.
type MDNSService struct {
	server *zeroconf.Server
}

// StartMDNSService advertises the node identity and listening port on the local subnet.
func StartMDNSService(peerID string, port int) (*MDNSService, error) {
	instanceName := peerID
	if len(instanceName) > 63 {
		instanceName = peerID[:63] // DNS label length restriction
	}

	txt := FormatServiceTXT(peerID, ProtocolVer)
	server, err := zeroconf.Register(
		instanceName,
		ServiceType,
		ServiceDomain,
		port,
		txt,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register mDNS service: %w", err)
	}

	return &MDNSService{server: server}, nil
}

// Shutdown stops the mDNS advertisement.
func (s *MDNSService) Shutdown() {
	if s != nil && s.server != nil {
		s.server.Shutdown()
	}
}

// DiscoverPeers browses for local peers and registers them into the peer registry.
func DiscoverPeers(ctx context.Context, reg *PeerRegistry, localPeerID string) error {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return fmt.Errorf("failed to initialize zeroconf resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for entry := range entries {
			peerID, _, err := ParseServiceTXT(entry.Text)
			if err != nil || peerID == localPeerID {
				continue
			}

			// Extract best IP address
			var ip net.IP
			if len(entry.AddrIPv4) > 0 {
				ip = entry.AddrIPv4[0]
			} else if len(entry.AddrIPv6) > 0 {
				ip = entry.AddrIPv6[0]
			}

			if ip != nil {
				endpoint := fmt.Sprintf("%s:%d", ip.String(), entry.Port)
				reg.RegisterPeer(peerID, endpoint, DiscoveryMDNS)
			}
		}
	}()

	err = resolver.Browse(ctx, ServiceType, ServiceDomain, entries)
	if err != nil {
		return fmt.Errorf("failed to browse mDNS: %w", err)
	}

	return nil
}
