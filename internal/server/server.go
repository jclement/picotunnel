package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jclement/picotunnel/internal/server/web"
)

// Config holds server configuration
type Config struct {
	ListenAddr   string
	TunnelAddr   string
	HTTPAddr     string
	HTTPSAddr    string
	DataDir      string
	Domain       string
	
	// OIDC config
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	
	// TLS/ACME config
	ACMEEnabled bool
	ACMEEmail   string
}

// Server represents the main server
type Server struct {
	config        Config
	store         *Store
	tunnelManager *TunnelManager
	proxyManager  *ProxyManager
	authHandler   *AuthHandler
	tlsManager    *TLSManager
	apiHandler    *APIHandler
	
	httpServer   *http.Server
	tunnelServer *http.Server
}

// NewServer creates a new server
func NewServer(config Config) (*Server, error) {
	// Ensure data directory exists
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize store
	dbPath := filepath.Join(config.DataDir, "picotunnel.db")
	store, err := NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize store: %w", err)
	}

	// Initialize tunnel manager
	tunnelManager := NewTunnelManager(store)

	// Initialize proxy manager
	proxyManager := NewProxyManager(store, tunnelManager)

	// Initialize auth handler
	authConfig := AuthConfig{
		Issuer:       config.OIDCIssuer,
		ClientID:     config.OIDCClientID,
		ClientSecret: config.OIDCClientSecret,
		RedirectURL:  config.OIDCRedirectURL,
	}
	authHandler, err := NewAuthHandler(authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize auth handler: %w", err)
	}

	// Initialize TLS manager
	tlsConfig := TLSConfig{
		Enabled:  config.ACMEEnabled,
		Email:    config.ACMEEmail,
		Domain:   config.Domain,
		CacheDir: config.DataDir,
	}
	tlsManager := NewTLSManager(tlsConfig)

	// Initialize API handler
	apiHandler := NewAPIHandler(store, tunnelManager, proxyManager)

	return &Server{
		config:        config,
		store:         store,
		tunnelManager: tunnelManager,
		proxyManager:  proxyManager,
		authHandler:   authHandler,
		tlsManager:    tlsManager,
		apiHandler:    apiHandler,
	}, nil
}

// Start starts the server
func (s *Server) Start() error {
	log.Printf("Starting PicoTunnel server")

	// Start tunnel manager
	if err := s.tunnelManager.Start(); err != nil {
		return fmt.Errorf("failed to start tunnel manager: %w", err)
	}

	// Start proxy manager
	if err := s.proxyManager.Start(s.config.HTTPAddr, s.config.HTTPSAddr); err != nil {
		return fmt.Errorf("failed to start proxy manager: %w", err)
	}

	// Setup HTTP routes
	mux := http.NewServeMux()
	
	// API routes
	s.apiHandler.RegisterRoutes(mux)
	
	// Auth routes
	s.authHandler.RegisterRoutes(mux)
	
	// Tunnel WebSocket endpoint
	mux.HandleFunc("GET /tunnel", s.tunnelManager.HandleWebSocket)
	
	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)
	
	// Static web assets (with auth middleware)
	webHandler := s.authHandler.Middleware(web.GetHandler())
	mux.Handle("GET /", webHandler)

	// Create HTTP server for management UI
	s.httpServer = &http.Server{
		Addr:    s.config.ListenAddr,
		Handler: mux,
	}

	// Start management server
	go func() {
		log.Printf("Management server listening on %s", s.config.ListenAddr)
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Printf("Management server error: %v", err)
		}
	}()

	// Setup tunnel WebSocket server (separate from management)
	tunnelMux := http.NewServeMux()
	tunnelMux.HandleFunc("GET /tunnel", s.tunnelManager.HandleWebSocket)
	
	s.tunnelServer = &http.Server{
		Addr:    s.config.TunnelAddr,
		Handler: tunnelMux,
	}

	// Start tunnel server
	go func() {
		log.Printf("Tunnel server listening on %s", s.config.TunnelAddr)
		if s.tlsManager.IsEnabled() {
			if err := s.tunnelServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
				log.Printf("Tunnel server TLS error: %v", err)
			}
		} else {
			if err := s.tunnelServer.ListenAndServe(); err != http.ErrServerClosed {
				log.Printf("Tunnel server error: %v", err)
			}
		}
	}()

	log.Printf("PicoTunnel server started successfully")
	return nil
}

// Stop stops the server
func (s *Server) Stop() error {
	log.Printf("Stopping PicoTunnel server")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop HTTP servers
	if s.httpServer != nil {
		s.httpServer.Shutdown(ctx)
	}
	if s.tunnelServer != nil {
		s.tunnelServer.Shutdown(ctx)
	}

	// Stop components
	if s.proxyManager != nil {
		s.proxyManager.Stop()
	}
	if s.tunnelManager != nil {
		s.tunnelManager.Stop()
	}

	// Close store
	if s.store != nil {
		s.store.Close()
	}

	log.Printf("PicoTunnel server stopped")
	return nil
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := struct {
		Status    string    `json:"status"`
		Timestamp time.Time `json:"timestamp"`
		Version   string    `json:"version"`
		Tunnels   int       `json:"connected_tunnels"`
	}{
		Status:    "ok",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Tunnels:   len(s.tunnelManager.ListConnectedTunnels()),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	// Simple JSON encoding
	fmt.Fprintf(w, `{
		"status": "%s",
		"timestamp": "%s",
		"version": "%s",
		"connected_tunnels": %d
	}`,
		status.Status,
		status.Timestamp.Format(time.RFC3339),
		status.Version,
		status.Tunnels,
	)
}