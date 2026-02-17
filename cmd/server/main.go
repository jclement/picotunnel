package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jclement/picotunnel/internal/server"
)

var (
	listenAddr  = flag.String("listen", getEnvOrDefault("PICOTUNNEL_LISTEN_ADDR", ":8080"), "Management UI listen address")
	tunnelAddr  = flag.String("tunnel", getEnvOrDefault("PICOTUNNEL_TUNNEL_ADDR", ":8443"), "Tunnel connection listen address")
	httpAddr    = flag.String("http", getEnvOrDefault("PICOTUNNEL_HTTP_ADDR", ":80"), "HTTP proxy listen address")
	httpsAddr   = flag.String("https", getEnvOrDefault("PICOTUNNEL_HTTPS_ADDR", ":443"), "HTTPS proxy listen address")
	dataDir     = flag.String("data", getEnvOrDefault("PICOTUNNEL_DATA_DIR", "/data"), "Data directory for database and certificates")
	domain      = flag.String("domain", getEnvOrDefault("PICOTUNNEL_DOMAIN", ""), "Server domain for management UI")
	
	// OIDC configuration
	oidcIssuer       = flag.String("oidc-issuer", getEnvOrDefault("PICOTUNNEL_OIDC_ISSUER", ""), "OIDC issuer URL")
	oidcClientID     = flag.String("oidc-client-id", getEnvOrDefault("PICOTUNNEL_OIDC_CLIENT_ID", ""), "OIDC client ID")
	oidcClientSecret = flag.String("oidc-client-secret", getEnvOrDefault("PICOTUNNEL_OIDC_CLIENT_SECRET", ""), "OIDC client secret")
	oidcRedirectURL  = flag.String("oidc-redirect", getEnvOrDefault("PICOTUNNEL_OIDC_REDIRECT_URL", ""), "OIDC redirect URL")
	
	// ACME/Let's Encrypt configuration
	acmeEnabled = flag.Bool("acme", getEnvOrDefault("PICOTUNNEL_ACME_ENABLED", "false") == "true", "Enable ACME/Let's Encrypt")
	acmeEmail   = flag.String("acme-email", getEnvOrDefault("PICOTUNNEL_ACME_EMAIL", ""), "ACME/Let's Encrypt email")
	
	version = flag.Bool("version", false, "Show version")
)

func main() {
	flag.Parse()

	if *version {
		fmt.Println("picotunnel-server v1.0.0")
		os.Exit(0)
	}

	// Validate required configuration
	if *dataDir == "" {
		log.Fatal("Data directory is required")
	}

	if *acmeEnabled && *acmeEmail == "" {
		log.Fatal("ACME email is required when ACME is enabled")
	}

	if (*oidcIssuer != "" || *oidcClientID != "") && (*oidcIssuer == "" || *oidcClientID == "") {
		log.Fatal("Both OIDC issuer and client ID are required for authentication")
	}

	log.Printf("Starting PicoTunnel server with config:")
	log.Printf("  Listen: %s", *listenAddr)
	log.Printf("  Tunnel: %s", *tunnelAddr)
	log.Printf("  HTTP: %s", *httpAddr)
	log.Printf("  HTTPS: %s", *httpsAddr)
	log.Printf("  Data: %s", *dataDir)
	log.Printf("  Domain: %s", *domain)
	log.Printf("  OIDC: %s", enabledStr(*oidcIssuer != ""))
	log.Printf("  ACME: %s", enabledStr(*acmeEnabled))

	// Create server configuration
	config := server.Config{
		ListenAddr:       *listenAddr,
		TunnelAddr:       *tunnelAddr,
		HTTPAddr:         *httpAddr,
		HTTPSAddr:        *httpsAddr,
		DataDir:          *dataDir,
		Domain:           *domain,
		OIDCIssuer:       *oidcIssuer,
		OIDCClientID:     *oidcClientID,
		OIDCClientSecret: *oidcClientSecret,
		OIDCRedirectURL:  *oidcRedirectURL,
		ACMEEnabled:      *acmeEnabled,
		ACMEEmail:        *acmeEmail,
	}

	// Create server
	srv, err := server.NewServer(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start server
	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	log.Printf("Server started successfully. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-sigChan
	log.Printf("Shutdown signal received")

	// Stop server
	if err := srv.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Printf("Server stopped")
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func enabledStr(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}