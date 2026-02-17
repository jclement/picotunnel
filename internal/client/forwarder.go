package client

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/jclement/picotunnel/internal/tunnel"
)

// Forwarder handles forwarding streams to local services
type Forwarder struct {
	// Add any configuration here if needed
}

// NewForwarder creates a new forwarder
func NewForwarder() *Forwarder {
	return &Forwarder{}
}

// HandleStream implements tunnel.StreamHandler
func (f *Forwarder) HandleStream(stream net.Conn, header tunnel.StreamHeader) error {
	defer stream.Close()

	log.Printf("Handling %s stream to %s", header.Type, header.Target)

	// Connect to local target
	targetConn, err := net.DialTimeout("tcp", header.Target, time.Second*10)
	if err != nil {
		log.Printf("Failed to connect to target %s: %v", header.Target, err)
		return fmt.Errorf("failed to connect to target %s: %w", header.Target, err)
	}
	defer targetConn.Close()

	log.Printf("Connected to target %s, starting proxy", header.Target)

	// Start bidirectional copy
	if err := tunnel.CopyBidirectional(stream, targetConn); err != nil {
		log.Printf("Proxy error for %s: %v", header.Target, err)
		return err
	}

	log.Printf("Stream to %s completed", header.Target)
	return nil
}