package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/jclement/picotunnel/internal/models"
	"github.com/jclement/picotunnel/internal/tunnel"
)

// ProxyManager handles HTTP and TCP proxying
type ProxyManager struct {
	store         *Store
	tunnelManager *TunnelManager
	httpServer    *http.Server
	httpsServer   *http.Server
	tcpListeners  map[string]net.Listener // listenAddr -> listener
}

// NewProxyManager creates a new proxy manager
func NewProxyManager(store *Store, tunnelManager *TunnelManager) *ProxyManager {
	return &ProxyManager{
		store:         store,
		tunnelManager: tunnelManager,
		tcpListeners:  make(map[string]net.Listener),
	}
}

// Start starts the proxy servers
func (pm *ProxyManager) Start(httpAddr, httpsAddr string) error {
	log.Printf("Starting proxy manager")

	// Start HTTP proxy
	if httpAddr != "" {
		pm.httpServer = &http.Server{
			Addr:    httpAddr,
			Handler: http.HandlerFunc(pm.handleHTTP),
		}

		go func() {
			log.Printf("HTTP proxy listening on %s", httpAddr)
			if err := pm.httpServer.ListenAndServe(); err != http.ErrServerClosed {
				log.Printf("HTTP proxy error: %v", err)
			}
		}()
	}

	// Start HTTPS proxy (if configured)
	if httpsAddr != "" {
		pm.httpsServer = &http.Server{
			Addr:    httpsAddr,
			Handler: http.HandlerFunc(pm.handleHTTPS),
		}

		// Note: TLS configuration would be added here for Let's Encrypt
		// For now, we'll skip HTTPS until TLS implementation is ready
		log.Printf("HTTPS proxy disabled pending TLS implementation")
	}

	// Start TCP listeners for all TCP services
	if err := pm.startTCPListeners(); err != nil {
		return fmt.Errorf("failed to start TCP listeners: %w", err)
	}

	return nil
}

// Stop stops the proxy servers
func (pm *ProxyManager) Stop() error {
	log.Printf("Stopping proxy manager")

	// Stop HTTP server
	if pm.httpServer != nil {
		pm.httpServer.Close()
	}

	// Stop HTTPS server
	if pm.httpsServer != nil {
		pm.httpsServer.Close()
	}

	// Stop TCP listeners
	for addr, listener := range pm.tcpListeners {
		log.Printf("Stopping TCP listener on %s", addr)
		listener.Close()
	}

	return nil
}

// handleHTTP handles HTTP requests
func (pm *ProxyManager) handleHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if host == "" {
		http.Error(w, "Missing Host header", http.StatusBadRequest)
		return
	}

	// Remove port if present
	if colonIndex := strings.LastIndex(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}

	log.Printf("HTTP request for %s %s", host, r.URL.Path)

	// Find service by domain
	service, err := pm.store.GetServiceByDomain(host)
	if err != nil {
		log.Printf("Service not found for domain %s: %v", host, err)
		http.Error(w, "Service not found", http.StatusNotFound)
		return
	}

	// Check if service is enabled
	if !service.Enabled {
		log.Printf("Service for domain %s is disabled", host)
		http.Error(w, "Service disabled", http.StatusServiceUnavailable)
		return
	}

	// Check path prefix
	if service.PathPrefix != "/" && !strings.HasPrefix(r.URL.Path, service.PathPrefix) {
		log.Printf("Path %s does not match prefix %s for domain %s", r.URL.Path, service.PathPrefix, host)
		http.Error(w, "Path not found", http.StatusNotFound)
		return
	}

	// Get tunnel connection
	conn, exists := pm.tunnelManager.GetConnection(service.TunnelID)
	if !exists {
		log.Printf("Tunnel %s for domain %s is not connected", service.TunnelID, host)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	// Open stream to client
	stream, err := conn.Open()
	if err != nil {
		log.Printf("Failed to open stream for domain %s: %v", host, err)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}
	defer stream.Close()

	// Send stream header
	header := tunnel.StreamHeader{
		Type:   "http",
		Target: service.TargetAddr,
	}

	streamWithHeader, err := pm.wrapStreamWithHeader(stream, header)
	if err != nil {
		log.Printf("Failed to send stream header for domain %s: %v", host, err)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	// Create reverse proxy
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			// Preserve original request details
			req.URL.Scheme = "http"
			req.URL.Host = service.TargetAddr
		},
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return streamWithHeader, nil
			},
		},
	}

	log.Printf("Proxying HTTP request for %s to %s", host, service.TargetAddr)
	proxy.ServeHTTP(w, r)
}

// handleHTTPS handles HTTPS requests (SNI-based routing)
func (pm *ProxyManager) handleHTTPS(w http.ResponseWriter, r *http.Request) {
	// For now, redirect to HTTP
	// This would be replaced with actual TLS handling
	http.Redirect(w, r, "http://"+r.Host+r.URL.Path, http.StatusTemporaryRedirect)
}

// startTCPListeners starts TCP listeners for all TCP services
func (pm *ProxyManager) startTCPListeners() error {
	// Get all tunnels and their services
	tunnels, err := pm.store.ListTunnels()
	if err != nil {
		return fmt.Errorf("failed to list tunnels: %w", err)
	}

	for _, tunnel := range tunnels {
		services, err := pm.store.ListServices(tunnel.ID)
		if err != nil {
			log.Printf("Failed to list services for tunnel %s: %v", tunnel.ID, err)
			continue
		}

		for _, service := range services {
			if service.Type == "tcp" && service.Enabled && service.ListenAddr != "" {
				if err := pm.startTCPListener(service); err != nil {
					log.Printf("Failed to start TCP listener for service %s: %v", service.ID, err)
				}
			}
		}
	}

	return nil
}

// startTCPListener starts a TCP listener for a service
func (pm *ProxyManager) startTCPListener(service *models.Service) error {
	// Check if listener already exists
	if _, exists := pm.tcpListeners[service.ListenAddr]; exists {
		return fmt.Errorf("TCP listener already exists for %s", service.ListenAddr)
	}

	listener, err := net.Listen("tcp", service.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", service.ListenAddr, err)
	}

	pm.tcpListeners[service.ListenAddr] = listener

	log.Printf("TCP listener started on %s for service %s", service.ListenAddr, service.ID)

	// Handle connections in a goroutine
	go pm.handleTCPListener(listener, service)

	return nil
}

// handleTCPListener handles connections for a TCP listener
func (pm *ProxyManager) handleTCPListener(listener net.Listener, service *models.Service) {
	defer listener.Close()

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("TCP accept error for %s: %v", service.ListenAddr, err)
			return
		}

		go pm.handleTCPConnection(clientConn, service)
	}
}

// handleTCPConnection handles a single TCP connection
func (pm *ProxyManager) handleTCPConnection(clientConn net.Conn, service *models.Service) {
	defer clientConn.Close()

	log.Printf("TCP connection from %s to %s", clientConn.RemoteAddr(), service.ListenAddr)

	// Get tunnel connection
	conn, exists := pm.tunnelManager.GetConnection(service.TunnelID)
	if !exists {
		log.Printf("Tunnel %s for TCP service is not connected", service.TunnelID)
		return
	}

	// Open stream to client
	stream, err := conn.Open()
	if err != nil {
		log.Printf("Failed to open stream for TCP service: %v", err)
		return
	}
	defer stream.Close()

	// Send stream header
	header := tunnel.StreamHeader{
		Type:   "tcp",
		Target: service.TargetAddr,
	}

	streamWithHeader, err := pm.wrapStreamWithHeader(stream, header)
	if err != nil {
		log.Printf("Failed to send stream header for TCP service: %v", err)
		return
	}

	log.Printf("Proxying TCP connection to %s", service.TargetAddr)

	// Copy data bidirectionally
	if err := tunnel.CopyBidirectional(clientConn, streamWithHeader); err != nil {
		log.Printf("TCP proxy error: %v", err)
	}

	log.Printf("TCP connection closed")
}

// wrapStreamWithHeader wraps a stream with header sending
func (pm *ProxyManager) wrapStreamWithHeader(stream net.Conn, header tunnel.StreamHeader) (net.Conn, error) {
	sm := tunnel.NewStreamManager(&tunnel.Connection{}) // Dummy connection for header sending
	return sm.OpenStream(header)
}

// AddTCPService adds a new TCP service and starts its listener
func (pm *ProxyManager) AddTCPService(service *models.Service) error {
	if service.Type == "tcp" && service.Enabled && service.ListenAddr != "" {
		return pm.startTCPListener(service)
	}
	return nil
}

// RemoveTCPService removes a TCP service and stops its listener
func (pm *ProxyManager) RemoveTCPService(service *models.Service) error {
	if listener, exists := pm.tcpListeners[service.ListenAddr]; exists {
		listener.Close()
		delete(pm.tcpListeners, service.ListenAddr)
		log.Printf("Stopped TCP listener on %s", service.ListenAddr)
	}
	return nil
}