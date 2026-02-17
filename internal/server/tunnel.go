package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jclement/picotunnel/internal/models"
	"github.com/jclement/picotunnel/internal/tunnel"
)

// TunnelManager manages tunnel connections
type TunnelManager struct {
	store       *Store
	connections map[string]*tunnel.Connection // tunnelID -> connection
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewTunnelManager creates a new tunnel manager
func NewTunnelManager(store *Store) *TunnelManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &TunnelManager{
		store:       store,
		connections: make(map[string]*tunnel.Connection),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start starts the tunnel manager
func (tm *TunnelManager) Start() error {
	log.Printf("Starting tunnel manager")

	// Start health check routine
	tm.wg.Add(1)
	go tm.healthCheckRoutine()

	// Start cleanup routine
	tm.wg.Add(1)
	go tm.cleanupRoutine()

	return nil
}

// Stop stops the tunnel manager
func (tm *TunnelManager) Stop() error {
	log.Printf("Stopping tunnel manager")

	tm.cancel()

	// Close all connections
	tm.mu.Lock()
	for _, conn := range tm.connections {
		conn.Close()
	}
	tm.mu.Unlock()

	tm.wg.Wait()
	return nil
}

// HandleWebSocket handles a new WebSocket connection
func (tm *TunnelManager) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract token from query parameters
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing token", http.StatusBadRequest)
		return
	}

	// Validate token
	tunnelObj, err := tm.store.GetTunnelByToken(token)
	if err != nil {
		log.Printf("Invalid token %s: %v", token, err)
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	// Upgrade to WebSocket
	ws, err := tunnel.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	log.Printf("New tunnel connection for tunnel %s (%s)", tunnelObj.Name, tunnelObj.ID)

	// Create tunnel connection
	conn, err := tunnel.NewConnection(ws, token, true)
	if err != nil {
		log.Printf("Failed to create tunnel connection: %v", err)
		ws.Close()
		return
	}

	// Register connection
	tm.registerConnection(tunnelObj.ID, conn)
	defer tm.unregisterConnection(tunnelObj.ID)

	// Record connection
	check := &models.Check{
		TunnelID:  tunnelObj.ID,
		Status:    "up",
		CreatedAt: time.Now(),
	}
	if err := tm.store.CreateCheck(check); err != nil {
		log.Printf("Failed to record connection check: %v", err)
	}

	// Handle connection
	tm.handleConnection(tunnelObj.ID, conn)
}

// registerConnection registers a new tunnel connection
func (tm *TunnelManager) registerConnection(tunnelID string, conn *tunnel.Connection) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Close existing connection if any
	if existing := tm.connections[tunnelID]; existing != nil {
		log.Printf("Closing existing connection for tunnel %s", tunnelID)
		existing.Close()
	}

	tm.connections[tunnelID] = conn
	log.Printf("Registered connection for tunnel %s", tunnelID)
}

// unregisterConnection unregisters a tunnel connection
func (tm *TunnelManager) unregisterConnection(tunnelID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	delete(tm.connections, tunnelID)
	log.Printf("Unregistered connection for tunnel %s", tunnelID)

	// Record disconnection
	check := &models.Check{
		TunnelID:  tunnelID,
		Status:    "down",
		CreatedAt: time.Now(),
	}
	if err := tm.store.CreateCheck(check); err != nil {
		log.Printf("Failed to record disconnection check: %v", err)
	}
}

// handleConnection handles a tunnel connection
func (tm *TunnelManager) handleConnection(tunnelID string, conn *tunnel.Connection) {
	defer conn.Close()

	// Handle control messages in a separate goroutine
	tm.wg.Add(1)
	go func() {
		defer tm.wg.Done()
		tm.handleControlMessages(tunnelID, conn)
	}()

	// Keep connection alive until context is cancelled or connection closes
	for {
		select {
		case <-tm.ctx.Done():
			return
		default:
		}

		if conn.IsClosed() {
			return
		}

		time.Sleep(time.Second)
	}
}

// handleControlMessages handles control messages for a connection
func (tm *TunnelManager) handleControlMessages(tunnelID string, conn *tunnel.Connection) {
	for {
		select {
		case <-tm.ctx.Done():
			return
		default:
		}

		if conn.IsClosed() {
			return
		}

		msg, err := conn.ReadMessage()
		if err != nil {
			if tm.ctx.Err() == nil {
				log.Printf("Error reading control message for tunnel %s: %v", tunnelID, err)
			}
			return
		}

		switch msg.Type {
		case "ping":
			start := time.Now()
			if err := conn.Pong(); err != nil {
				log.Printf("Failed to send pong to tunnel %s: %v", tunnelID, err)
				return
			}
			latency := int(time.Since(start).Milliseconds())

			// Record ping response
			check := &models.Check{
				TunnelID:  tunnelID,
				Status:    "up",
				LatencyMs: &latency,
				CreatedAt: time.Now(),
			}
			if err := tm.store.CreateCheck(check); err != nil {
				log.Printf("Failed to record ping check: %v", err)
			}

		case "pong":
			conn.UpdateLastPing()
		default:
			log.Printf("Unknown control message type from tunnel %s: %s", tunnelID, msg.Type)
		}
	}
}

// GetConnection returns a connection for a tunnel ID
func (tm *TunnelManager) GetConnection(tunnelID string) (*tunnel.Connection, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	conn, exists := tm.connections[tunnelID]
	return conn, exists && !conn.IsClosed()
}

// IsConnected returns whether a tunnel is connected
func (tm *TunnelManager) IsConnected(tunnelID string) bool {
	_, connected := tm.GetConnection(tunnelID)
	return connected
}

// ListConnectedTunnels returns a list of connected tunnel IDs
func (tm *TunnelManager) ListConnectedTunnels() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var connected []string
	for tunnelID, conn := range tm.connections {
		if !conn.IsClosed() {
			connected = append(connected, tunnelID)
		}
	}

	return connected
}

// OpenStream opens a stream to a tunnel
func (tm *TunnelManager) OpenStream(tunnelID, target string, streamType string) (*tunnel.Connection, error) {
	conn, exists := tm.GetConnection(tunnelID)
	if !exists {
		return nil, fmt.Errorf("tunnel %s is not connected", tunnelID)
	}

	return conn, nil
}

// healthCheckRoutine performs periodic health checks
func (tm *TunnelManager) healthCheckRoutine() {
	defer tm.wg.Done()

	ticker := time.NewTicker(tunnel.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			tm.performHealthChecks()
		}
	}
}

// performHealthChecks checks the health of all connections
func (tm *TunnelManager) performHealthChecks() {
	tm.mu.RLock()
	connections := make(map[string]*tunnel.Connection)
	for id, conn := range tm.connections {
		connections[id] = conn
	}
	tm.mu.RUnlock()

	for tunnelID, conn := range connections {
		if conn.IsClosed() {
			continue
		}

		// Check if connection is stale
		if time.Since(conn.LastPing()) > tunnel.PingInterval*3 {
			log.Printf("Connection for tunnel %s is stale, closing", tunnelID)
			conn.Close()
			
			// Record as down
			check := &models.Check{
				TunnelID:  tunnelID,
				Status:    "down",
				Error:     stringPtr("Connection timeout"),
				CreatedAt: time.Now(),
			}
			if err := tm.store.CreateCheck(check); err != nil {
				log.Printf("Failed to record timeout check: %v", err)
			}
			continue
		}

		// Send ping
		if err := conn.Ping(); err != nil {
			log.Printf("Failed to ping tunnel %s: %v", tunnelID, err)
			conn.Close()
			
			// Record as down
			check := &models.Check{
				TunnelID:  tunnelID,
				Status:    "down",
				Error:     stringPtr(err.Error()),
				CreatedAt: time.Now(),
			}
			if err := tm.store.CreateCheck(check); err != nil {
				log.Printf("Failed to record ping failure check: %v", err)
			}
		}
	}
}

// cleanupRoutine performs periodic cleanup
func (tm *TunnelManager) cleanupRoutine() {
	defer tm.wg.Done()

	ticker := time.NewTicker(time.Hour * 24) // Daily cleanup
	defer ticker.Stop()

	for {
		select {
		case <-tm.ctx.Done():
			return
		case <-ticker.C:
			// Cleanup old checks (older than 6 months)
			if err := tm.store.CleanupOldChecks(time.Hour * 24 * 30 * 6); err != nil {
				log.Printf("Failed to cleanup old checks: %v", err)
			} else {
				log.Printf("Cleaned up old uptime checks")
			}
		}
	}
}

// stringPtr returns a pointer to a string
func stringPtr(s string) *string {
	return &s
}