package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// AuthConfig holds OIDC configuration
type AuthConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// AuthHandler handles OIDC authentication
type AuthHandler struct {
	config   AuthConfig
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth2   oauth2.Config
	enabled  bool
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(config AuthConfig) (*AuthHandler, error) {
	if config.Issuer == "" || config.ClientID == "" {
		return &AuthHandler{enabled: false}, nil
	}

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, config.Issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: config.ClientID,
	})

	oauth2Config := oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		RedirectURL:  config.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	return &AuthHandler{
		config:   config,
		provider: provider,
		verifier: verifier,
		oauth2:   oauth2Config,
		enabled:  true,
	}, nil
}

// IsEnabled returns whether authentication is enabled
func (ah *AuthHandler) IsEnabled() bool {
	return ah.enabled
}

// Middleware creates an authentication middleware
func (ah *AuthHandler) Middleware(next http.Handler) http.Handler {
	if !ah.enabled {
		return next // No auth required
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for certain paths
		if ah.shouldSkipAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Check for valid session
		if !ah.isAuthenticated(r) {
			// Redirect to login
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RegisterRoutes registers authentication routes
func (ah *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	if !ah.enabled {
		return
	}

	mux.HandleFunc("GET /auth/login", ah.handleLogin)
	mux.HandleFunc("GET /auth/callback", ah.handleCallback)
	mux.HandleFunc("POST /auth/logout", ah.handleLogout)
}

// shouldSkipAuth determines if authentication should be skipped for a path
func (ah *AuthHandler) shouldSkipAuth(path string) bool {
	skipPaths := []string{
		"/tunnel",        // WebSocket tunnel endpoint
		"/auth/login",    // Auth endpoints
		"/auth/callback",
		"/auth/logout",
		"/health",        // Health check
	}

	for _, skipPath := range skipPaths {
		if path == skipPath {
			return true
		}
	}

	return false
}

// isAuthenticated checks if the request is authenticated
func (ah *AuthHandler) isAuthenticated(r *http.Request) bool {
	// Check for valid session cookie
	cookie, err := r.Cookie("picotunnel_session")
	if err != nil {
		return false
	}

	// For now, just check if cookie exists
	// In a real implementation, this would verify the session token
	return cookie.Value != ""
}

// handleLogin handles the login redirect
func (ah *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Generate state parameter for CSRF protection
	state := "random-state" // In production, this should be cryptographically random
	
	url := ah.oauth2.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// handleCallback handles the OAuth callback
func (ah *AuthHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing code parameter", http.StatusBadRequest)
		return
	}

	// Exchange code for token
	token, err := ah.oauth2.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "Failed to exchange code", http.StatusInternalServerError)
		return
	}

	// Verify ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "Missing id_token", http.StatusInternalServerError)
		return
	}

	idToken, err := ah.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, "Failed to verify ID token", http.StatusInternalServerError)
		return
	}

	// Extract claims
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "Failed to parse claims", http.StatusInternalServerError)
		return
	}

	// Set session cookie
	cookie := &http.Cookie{
		Name:     "picotunnel_session",
		Value:    rawIDToken, // In production, use a secure session ID
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		MaxAge:   int(24 * time.Hour.Seconds()),
	}
	http.SetCookie(w, cookie)

	// Redirect to dashboard
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// handleLogout handles logout
func (ah *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear session cookie
	cookie := &http.Cookie{
		Name:   "picotunnel_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	}
	http.SetCookie(w, cookie)

	// Redirect to login
	http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
}