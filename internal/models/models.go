package models

import (
	"time"
)

// Tunnel represents a tunnel configuration
type Tunnel struct {
	ID        string    `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Token     string    `json:"token" db:"token"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
	Connected bool      `json:"connected" db:"-"` // Runtime status, not stored
}

// Service represents a service within a tunnel
type Service struct {
	ID          string    `json:"id" db:"id"`
	TunnelID    string    `json:"tunnel_id" db:"tunnel_id"`
	Type        string    `json:"type" db:"type"` // "http" or "tcp"
	Domain      string    `json:"domain" db:"domain"`
	PathPrefix  string    `json:"path_prefix" db:"path_prefix"`
	TLSMode     string    `json:"tls_mode" db:"tls_mode"`     // "terminate" or "passthrough"
	ListenAddr  string    `json:"listen_addr" db:"listen_addr"` // for TCP services
	TargetAddr  string    `json:"target_addr" db:"target_addr"`
	Enabled     bool      `json:"enabled" db:"enabled"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// Check represents an uptime check result
type Check struct {
	ID        int       `json:"id" db:"id"`
	TunnelID  string    `json:"tunnel_id" db:"tunnel_id"`
	Status    string    `json:"status" db:"status"` // "up" or "down"
	LatencyMs *int      `json:"latency_ms" db:"latency_ms"`
	Error     *string   `json:"error" db:"error"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// TunnelMessage represents the message format for tunnel protocol
type TunnelMessage struct {
	Type   string `json:"type"`   // "stream", "ping", "pong"
	Target string `json:"target"` // target address for stream connections
}

// UptimeStats represents uptime statistics
type UptimeStats struct {
	Uptime24h float64 `json:"uptime_24h"`
	Uptime7d  float64 `json:"uptime_7d"`
	Uptime30d float64 `json:"uptime_30d"`
}