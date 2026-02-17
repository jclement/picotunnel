package server

import (
	"crypto/tls"
	"net/http"
	"path/filepath"

	"golang.org/x/crypto/acme/autocert"
)

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enabled   bool
	Email     string
	Domain    string
	CacheDir  string
}

// TLSManager manages TLS certificates
type TLSManager struct {
	config    TLSConfig
	certMgr   *autocert.Manager
	tlsConfig *tls.Config
}

// NewTLSManager creates a new TLS manager
func NewTLSManager(config TLSConfig) *TLSManager {
	if !config.Enabled {
		return &TLSManager{config: config}
	}

	certMgr := &autocert.Manager{
		Cache:      autocert.DirCache(filepath.Join(config.CacheDir, "certs")),
		Prompt:     autocert.AcceptTOS,
		Email:      config.Email,
		HostPolicy: autocert.HostWhitelist(config.Domain),
	}

	tlsConfig := &tls.Config{
		GetCertificate: certMgr.GetCertificate,
		NextProtos:     []string{"h2", "http/1.1"},
	}

	return &TLSManager{
		config:    config,
		certMgr:   certMgr,
		tlsConfig: tlsConfig,
	}
}

// IsEnabled returns whether TLS is enabled
func (tm *TLSManager) IsEnabled() bool {
	return tm.config.Enabled
}

// GetTLSConfig returns the TLS configuration
func (tm *TLSManager) GetTLSConfig() *tls.Config {
	return tm.tlsConfig
}

// GetACMEHandler returns the ACME HTTP handler for HTTP-01 challenges
func (tm *TLSManager) GetACMEHandler() http.Handler {
	if tm.certMgr == nil {
		return nil
	}
	return tm.certMgr.HTTPHandler(nil)
}

// ValidateDomain checks if a domain is allowed for certificate issuance
func (tm *TLSManager) ValidateDomain(domain string) error {
	if !tm.config.Enabled {
		return nil
	}

	if tm.certMgr.HostPolicy == nil {
		return nil
	}

	return tm.certMgr.HostPolicy(nil, domain)
}