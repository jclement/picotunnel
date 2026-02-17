package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// StreamHeader contains metadata for a stream
type StreamHeader struct {
	Type   string `json:"type"`   // "http" or "tcp"
	Target string `json:"target"` // target address to forward to
}

// StreamManager manages multiple streams over a tunnel connection
type StreamManager struct {
	conn     *Connection
	handlers map[string]StreamHandler
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// StreamHandler handles a specific type of stream
type StreamHandler interface {
	HandleStream(stream net.Conn, header StreamHeader) error
}

// NewStreamManager creates a new stream manager
func NewStreamManager(conn *Connection) *StreamManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &StreamManager{
		conn:     conn,
		handlers: make(map[string]StreamHandler),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// RegisterHandler registers a handler for a stream type
func (sm *StreamManager) RegisterHandler(streamType string, handler StreamHandler) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.handlers[streamType] = handler
}

// Start starts handling streams
func (sm *StreamManager) Start() error {
	sm.wg.Add(1)
	go sm.handleStreams()
	
	// Start ping routine
	sm.wg.Add(1)
	go sm.pingRoutine()

	return nil
}

// Stop stops the stream manager
func (sm *StreamManager) Stop() error {
	sm.cancel()
	sm.wg.Wait()
	return sm.conn.Close()
}

// OpenStream opens a new stream with header
func (sm *StreamManager) OpenStream(header StreamHeader) (net.Conn, error) {
	stream, err := sm.conn.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}

	// Send header
	headerData, err := json.Marshal(header)
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("failed to marshal header: %w", err)
	}

	headerLen := len(headerData)
	if headerLen > 0xFFFF {
		stream.Close()
		return nil, fmt.Errorf("header too large: %d bytes", headerLen)
	}

	// Write header length (2 bytes) + header + newline
	buf := make([]byte, 2+headerLen+1)
	buf[0] = byte(headerLen >> 8)
	buf[1] = byte(headerLen & 0xFF)
	copy(buf[2:], headerData)
	buf[2+headerLen] = '\n'

	_, err = stream.Write(buf)
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("failed to write header: %w", err)
	}

	return stream, nil
}

// handleStreams handles incoming streams
func (sm *StreamManager) handleStreams() {
	defer sm.wg.Done()

	for {
		select {
		case <-sm.ctx.Done():
			return
		default:
		}

		stream, err := sm.conn.Accept()
		if err != nil {
			if sm.ctx.Err() != nil {
				return // Context cancelled
			}
			continue
		}

		sm.wg.Add(1)
		go func(stream net.Conn) {
			defer sm.wg.Done()
			defer stream.Close()

			if err := sm.handleStream(stream); err != nil {
				// Log error but continue
			}
		}(stream)
	}
}

// handleStream processes a single stream
func (sm *StreamManager) handleStream(stream net.Conn) error {
	// Read header length (2 bytes)
	headerLenBuf := make([]byte, 2)
	_, err := io.ReadFull(stream, headerLenBuf)
	if err != nil {
		return fmt.Errorf("failed to read header length: %w", err)
	}

	headerLen := int(headerLenBuf[0])<<8 | int(headerLenBuf[1])
	if headerLen == 0 || headerLen > 0xFFFF {
		return fmt.Errorf("invalid header length: %d", headerLen)
	}

	// Read header + newline
	headerBuf := make([]byte, headerLen+1)
	_, err = io.ReadFull(stream, headerBuf)
	if err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	if headerBuf[headerLen] != '\n' {
		return fmt.Errorf("header not terminated with newline")
	}

	// Parse header
	var header StreamHeader
	err = json.Unmarshal(headerBuf[:headerLen], &header)
	if err != nil {
		return fmt.Errorf("failed to unmarshal header: %w", err)
	}

	// Find handler
	sm.mu.RLock()
	handler, exists := sm.handlers[header.Type]
	sm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no handler for stream type: %s", header.Type)
	}

	// Handle stream
	return handler.HandleStream(stream, header)
}

// pingRoutine sends periodic pings
func (sm *StreamManager) pingRoutine() {
	defer sm.wg.Done()

	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return
		case <-ticker.C:
			if err := sm.conn.Ping(); err != nil {
				return
			}
		}
	}
}

// CopyBidirectional copies data bidirectionally between two connections
func CopyBidirectional(conn1, conn2 net.Conn) error {
	var wg sync.WaitGroup
	var err1, err2 error

	wg.Add(2)

	go func() {
		defer wg.Done()
		defer conn1.Close()
		defer conn2.Close()
		_, err1 = io.Copy(conn1, conn2)
	}()

	go func() {
		defer wg.Done()
		defer conn1.Close()
		defer conn2.Close()
		_, err2 = io.Copy(conn2, conn1)
	}()

	wg.Wait()

	if err1 != nil {
		return err1
	}
	return err2
}