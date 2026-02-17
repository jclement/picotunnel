package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jclement/picotunnel/internal/models"
)

// APIHandler handles REST API requests
type APIHandler struct {
	store         *Store
	tunnelManager *TunnelManager
	proxyManager  *ProxyManager
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(store *Store, tunnelManager *TunnelManager, proxyManager *ProxyManager) *APIHandler {
	return &APIHandler{
		store:         store,
		tunnelManager: tunnelManager,
		proxyManager:  proxyManager,
	}
}

// RegisterRoutes registers API routes
func (api *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	// Tunnel routes
	mux.HandleFunc("GET /api/tunnels", api.listTunnels)
	mux.HandleFunc("POST /api/tunnels", api.createTunnel)
	mux.HandleFunc("GET /api/tunnels/{id}", api.getTunnel)
	mux.HandleFunc("PATCH /api/tunnels/{id}", api.updateTunnel)
	mux.HandleFunc("DELETE /api/tunnels/{id}", api.deleteTunnel)
	mux.HandleFunc("POST /api/tunnels/{id}/regenerate", api.regenerateToken)

	// Service routes
	mux.HandleFunc("GET /api/tunnels/{id}/services", api.listServices)
	mux.HandleFunc("POST /api/tunnels/{id}/services", api.createService)
	mux.HandleFunc("PATCH /api/services/{id}", api.updateService)
	mux.HandleFunc("DELETE /api/services/{id}", api.deleteService)

	// Stats routes
	mux.HandleFunc("GET /api/tunnels/{id}/checks", api.getChecks)
	mux.HandleFunc("GET /api/tunnels/{id}/stats", api.getStats)
}

// TunnelResponse represents a tunnel with runtime status
type TunnelResponse struct {
	*models.Tunnel
	Connected bool `json:"connected"`
}

// listTunnels handles GET /api/tunnels
func (api *APIHandler) listTunnels(w http.ResponseWriter, r *http.Request) {
	tunnels, err := api.store.ListTunnels()
	if err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to list tunnels", err)
		return
	}

	// Add connection status
	var response []*TunnelResponse
	for _, tunnel := range tunnels {
		tr := &TunnelResponse{
			Tunnel:    tunnel,
			Connected: api.tunnelManager.IsConnected(tunnel.ID),
		}
		response = append(response, tr)
	}

	api.sendJSON(w, response)
}

// CreateTunnelRequest represents a request to create a tunnel
type CreateTunnelRequest struct {
	Name string `json:"name"`
}

// createTunnel handles POST /api/tunnels
func (api *APIHandler) createTunnel(w http.ResponseWriter, r *http.Request) {
	var req CreateTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.sendError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	if req.Name == "" {
		api.sendError(w, http.StatusBadRequest, "Name is required", nil)
		return
	}

	// Generate ID and token
	id, err := generateRandomID()
	if err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to generate ID", err)
		return
	}

	token, err := generateRandomToken()
	if err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to generate token", err)
		return
	}

	tunnel := &models.Tunnel{
		ID:        id,
		Name:      req.Name,
		Token:     token,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := api.store.CreateTunnel(tunnel); err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to create tunnel", err)
		return
	}

	response := &TunnelResponse{
		Tunnel:    tunnel,
		Connected: false,
	}

	w.WriteHeader(http.StatusCreated)
	api.sendJSON(w, response)
}

// getTunnel handles GET /api/tunnels/{id}
func (api *APIHandler) getTunnel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	tunnel, err := api.store.GetTunnel(id)
	if err != nil {
		api.sendError(w, http.StatusNotFound, "Tunnel not found", err)
		return
	}

	response := &TunnelResponse{
		Tunnel:    tunnel,
		Connected: api.tunnelManager.IsConnected(tunnel.ID),
	}

	api.sendJSON(w, response)
}

// UpdateTunnelRequest represents a request to update a tunnel
type UpdateTunnelRequest struct {
	Name string `json:"name"`
}

// updateTunnel handles PATCH /api/tunnels/{id}
func (api *APIHandler) updateTunnel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req UpdateTunnelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.sendError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	tunnel, err := api.store.GetTunnel(id)
	if err != nil {
		api.sendError(w, http.StatusNotFound, "Tunnel not found", err)
		return
	}

	if req.Name != "" {
		tunnel.Name = req.Name
		tunnel.UpdatedAt = time.Now()
	}

	if err := api.store.UpdateTunnel(tunnel); err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to update tunnel", err)
		return
	}

	response := &TunnelResponse{
		Tunnel:    tunnel,
		Connected: api.tunnelManager.IsConnected(tunnel.ID),
	}

	api.sendJSON(w, response)
}

// deleteTunnel handles DELETE /api/tunnels/{id}
func (api *APIHandler) deleteTunnel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Delete all services first (to stop TCP listeners)
	services, err := api.store.ListServices(id)
	if err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to list services", err)
		return
	}

	for _, service := range services {
		if err := api.proxyManager.RemoveTCPService(service); err != nil {
			// Log error but continue
			fmt.Printf("Failed to remove TCP service %s: %v", service.ID, err)
		}
	}

	if err := api.store.DeleteTunnel(id); err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to delete tunnel", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// regenerateToken handles POST /api/tunnels/{id}/regenerate
func (api *APIHandler) regenerateToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	tunnel, err := api.store.GetTunnel(id)
	if err != nil {
		api.sendError(w, http.StatusNotFound, "Tunnel not found", err)
		return
	}

	newToken, err := generateRandomToken()
	if err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to generate token", err)
		return
	}

	if err := api.store.UpdateTunnelToken(id, newToken); err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to update token", err)
		return
	}

	tunnel.Token = newToken
	tunnel.UpdatedAt = time.Now()

	response := &TunnelResponse{
		Tunnel:    tunnel,
		Connected: api.tunnelManager.IsConnected(tunnel.ID),
	}

	api.sendJSON(w, response)
}

// listServices handles GET /api/tunnels/{id}/services
func (api *APIHandler) listServices(w http.ResponseWriter, r *http.Request) {
	tunnelID := r.PathValue("id")

	services, err := api.store.ListServices(tunnelID)
	if err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to list services", err)
		return
	}

	api.sendJSON(w, services)
}

// CreateServiceRequest represents a request to create a service
type CreateServiceRequest struct {
	Type        string `json:"type"`        // "http" or "tcp"
	Domain      string `json:"domain"`      // for HTTP
	PathPrefix  string `json:"path_prefix"` // for HTTP
	TLSMode     string `json:"tls_mode"`    // for HTTP
	ListenAddr  string `json:"listen_addr"` // for TCP
	TargetAddr  string `json:"target_addr"`
	Enabled     bool   `json:"enabled"`
}

// createService handles POST /api/tunnels/{id}/services
func (api *APIHandler) createService(w http.ResponseWriter, r *http.Request) {
	tunnelID := r.PathValue("id")

	// Verify tunnel exists
	if _, err := api.store.GetTunnel(tunnelID); err != nil {
		api.sendError(w, http.StatusNotFound, "Tunnel not found", err)
		return
	}

	var req CreateServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.sendError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Validate request
	if req.Type != "http" && req.Type != "tcp" {
		api.sendError(w, http.StatusBadRequest, "Type must be 'http' or 'tcp'", nil)
		return
	}

	if req.TargetAddr == "" {
		api.sendError(w, http.StatusBadRequest, "Target address is required", nil)
		return
	}

	if req.Type == "http" && req.Domain == "" {
		api.sendError(w, http.StatusBadRequest, "Domain is required for HTTP services", nil)
		return
	}

	if req.Type == "tcp" && req.ListenAddr == "" {
		api.sendError(w, http.StatusBadRequest, "Listen address is required for TCP services", nil)
		return
	}

	// Set defaults
	if req.PathPrefix == "" {
		req.PathPrefix = "/"
	}
	if req.TLSMode == "" {
		req.TLSMode = "terminate"
	}

	id, err := generateRandomID()
	if err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to generate ID", err)
		return
	}

	service := &models.Service{
		ID:          id,
		TunnelID:    tunnelID,
		Type:        req.Type,
		Domain:      req.Domain,
		PathPrefix:  req.PathPrefix,
		TLSMode:     req.TLSMode,
		ListenAddr:  req.ListenAddr,
		TargetAddr:  req.TargetAddr,
		Enabled:     req.Enabled,
		CreatedAt:   time.Now(),
	}

	if err := api.store.CreateService(service); err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to create service", err)
		return
	}

	// Start TCP listener if needed
	if err := api.proxyManager.AddTCPService(service); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Failed to add TCP service %s: %v", service.ID, err)
	}

	w.WriteHeader(http.StatusCreated)
	api.sendJSON(w, service)
}

// UpdateServiceRequest represents a request to update a service
type UpdateServiceRequest struct {
	Domain     *string `json:"domain"`
	PathPrefix *string `json:"path_prefix"`
	TLSMode    *string `json:"tls_mode"`
	ListenAddr *string `json:"listen_addr"`
	TargetAddr *string `json:"target_addr"`
	Enabled    *bool   `json:"enabled"`
}

// updateService handles PATCH /api/services/{id}
func (api *APIHandler) updateService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	service, err := api.store.GetService(id)
	if err != nil {
		api.sendError(w, http.StatusNotFound, "Service not found", err)
		return
	}

	var req UpdateServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.sendError(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Remove old TCP listener if needed
	oldListenAddr := service.ListenAddr
	needsTCPRestart := false

	// Update fields
	if req.Domain != nil {
		service.Domain = *req.Domain
	}
	if req.PathPrefix != nil {
		service.PathPrefix = *req.PathPrefix
	}
	if req.TLSMode != nil {
		service.TLSMode = *req.TLSMode
	}
	if req.ListenAddr != nil {
		service.ListenAddr = *req.ListenAddr
		needsTCPRestart = service.Type == "tcp"
	}
	if req.TargetAddr != nil {
		service.TargetAddr = *req.TargetAddr
	}
	if req.Enabled != nil {
		service.Enabled = *req.Enabled
		needsTCPRestart = service.Type == "tcp"
	}

	if err := api.store.UpdateService(service); err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to update service", err)
		return
	}

	// Handle TCP listener changes
	if needsTCPRestart {
		// Remove old listener
		if oldListenAddr != "" {
			oldService := *service
			oldService.ListenAddr = oldListenAddr
			api.proxyManager.RemoveTCPService(&oldService)
		}

		// Add new listener
		if err := api.proxyManager.AddTCPService(service); err != nil {
			fmt.Printf("Failed to add TCP service %s: %v", service.ID, err)
		}
	}

	api.sendJSON(w, service)
}

// deleteService handles DELETE /api/services/{id}
func (api *APIHandler) deleteService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	service, err := api.store.GetService(id)
	if err != nil {
		api.sendError(w, http.StatusNotFound, "Service not found", err)
		return
	}

	// Remove TCP listener
	if err := api.proxyManager.RemoveTCPService(service); err != nil {
		fmt.Printf("Failed to remove TCP service %s: %v", service.ID, err)
	}

	if err := api.store.DeleteService(id); err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to delete service", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// getChecks handles GET /api/tunnels/{id}/checks
func (api *APIHandler) getChecks(w http.ResponseWriter, r *http.Request) {
	tunnelID := r.PathValue("id")

	limitStr := r.URL.Query().Get("limit")
	limit := 100 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	checks, err := api.store.GetChecks(tunnelID, limit)
	if err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to get checks", err)
		return
	}

	api.sendJSON(w, checks)
}

// getStats handles GET /api/tunnels/{id}/stats
func (api *APIHandler) getStats(w http.ResponseWriter, r *http.Request) {
	tunnelID := r.PathValue("id")

	stats, err := api.store.GetUptimeStats(tunnelID)
	if err != nil {
		api.sendError(w, http.StatusInternalServerError, "Failed to get stats", err)
		return
	}

	api.sendJSON(w, stats)
}

// Helper methods

// sendJSON sends a JSON response
func (api *APIHandler) sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// sendError sends an error response
func (api *APIHandler) sendError(w http.ResponseWriter, statusCode int, message string, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	errorMsg := message
	if err != nil {
		errorMsg = fmt.Sprintf("%s: %v", message, err)
	}

	response := ErrorResponse{
		Error:   http.StatusText(statusCode),
		Message: errorMsg,
	}

	json.NewEncoder(w).Encode(response)
}

// generateRandomID generates a random ID
func generateRandomID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// generateRandomToken generates a random token
func generateRandomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}