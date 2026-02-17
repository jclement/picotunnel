package server

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/jclement/picotunnel/internal/models"
)

// Store handles database operations
type Store struct {
	db *sql.DB
}

// NewStore creates a new store
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return store, nil
}

// Close closes the database connection
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the database schema
func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tunnels (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		token TEXT NOT NULL UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS services (
		id TEXT PRIMARY KEY,
		tunnel_id TEXT NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
		type TEXT NOT NULL CHECK(type IN ('http', 'tcp')),
		domain TEXT,
		path_prefix TEXT DEFAULT '/',
		tls_mode TEXT DEFAULT 'terminate' CHECK(tls_mode IN ('terminate', 'passthrough')),
		listen_addr TEXT,
		target_addr TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS checks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tunnel_id TEXT NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
		status TEXT NOT NULL CHECK(status IN ('up', 'down')),
		latency_ms INTEGER,
		error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_checks_tunnel_time ON checks(tunnel_id, created_at DESC);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Tunnel operations

// CreateTunnel creates a new tunnel
func (s *Store) CreateTunnel(tunnel *models.Tunnel) error {
	query := `
		INSERT INTO tunnels (id, name, token, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query, tunnel.ID, tunnel.Name, tunnel.Token, tunnel.CreatedAt, tunnel.UpdatedAt)
	return err
}

// GetTunnel gets a tunnel by ID
func (s *Store) GetTunnel(id string) (*models.Tunnel, error) {
	query := `SELECT id, name, token, created_at, updated_at FROM tunnels WHERE id = ?`
	
	var tunnel models.Tunnel
	err := s.db.QueryRow(query, id).Scan(
		&tunnel.ID, &tunnel.Name, &tunnel.Token, 
		&tunnel.CreatedAt, &tunnel.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	
	return &tunnel, nil
}

// GetTunnelByToken gets a tunnel by token
func (s *Store) GetTunnelByToken(token string) (*models.Tunnel, error) {
	query := `SELECT id, name, token, created_at, updated_at FROM tunnels WHERE token = ?`
	
	var tunnel models.Tunnel
	err := s.db.QueryRow(query, token).Scan(
		&tunnel.ID, &tunnel.Name, &tunnel.Token, 
		&tunnel.CreatedAt, &tunnel.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	
	return &tunnel, nil
}

// ListTunnels lists all tunnels
func (s *Store) ListTunnels() ([]*models.Tunnel, error) {
	query := `SELECT id, name, token, created_at, updated_at FROM tunnels ORDER BY name`
	
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tunnels []*models.Tunnel
	for rows.Next() {
		var tunnel models.Tunnel
		err := rows.Scan(
			&tunnel.ID, &tunnel.Name, &tunnel.Token,
			&tunnel.CreatedAt, &tunnel.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		tunnels = append(tunnels, &tunnel)
	}

	return tunnels, rows.Err()
}

// UpdateTunnel updates a tunnel
func (s *Store) UpdateTunnel(tunnel *models.Tunnel) error {
	query := `
		UPDATE tunnels 
		SET name = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query, tunnel.Name, time.Now(), tunnel.ID)
	return err
}

// UpdateTunnelToken updates a tunnel's token
func (s *Store) UpdateTunnelToken(id, token string) error {
	query := `UPDATE tunnels SET token = ?, updated_at = ? WHERE id = ?`
	_, err := s.db.Exec(query, token, time.Now(), id)
	return err
}

// DeleteTunnel deletes a tunnel and all its services
func (s *Store) DeleteTunnel(id string) error {
	query := `DELETE FROM tunnels WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// Service operations

// CreateService creates a new service
func (s *Store) CreateService(service *models.Service) error {
	query := `
		INSERT INTO services (id, tunnel_id, type, domain, path_prefix, tls_mode, listen_addr, target_addr, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query,
		service.ID, service.TunnelID, service.Type, service.Domain, service.PathPrefix,
		service.TLSMode, service.ListenAddr, service.TargetAddr, service.Enabled, service.CreatedAt,
	)
	return err
}

// GetService gets a service by ID
func (s *Store) GetService(id string) (*models.Service, error) {
	query := `
		SELECT id, tunnel_id, type, domain, path_prefix, tls_mode, listen_addr, target_addr, enabled, created_at
		FROM services WHERE id = ?
	`
	
	var service models.Service
	err := s.db.QueryRow(query, id).Scan(
		&service.ID, &service.TunnelID, &service.Type, &service.Domain, &service.PathPrefix,
		&service.TLSMode, &service.ListenAddr, &service.TargetAddr, &service.Enabled, &service.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	
	return &service, nil
}

// GetServiceByDomain gets an HTTP service by domain
func (s *Store) GetServiceByDomain(domain string) (*models.Service, error) {
	query := `
		SELECT id, tunnel_id, type, domain, path_prefix, tls_mode, listen_addr, target_addr, enabled, created_at
		FROM services WHERE type = 'http' AND domain = ? AND enabled = 1
	`
	
	var service models.Service
	err := s.db.QueryRow(query, domain).Scan(
		&service.ID, &service.TunnelID, &service.Type, &service.Domain, &service.PathPrefix,
		&service.TLSMode, &service.ListenAddr, &service.TargetAddr, &service.Enabled, &service.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	
	return &service, nil
}

// ListServices lists services for a tunnel
func (s *Store) ListServices(tunnelID string) ([]*models.Service, error) {
	query := `
		SELECT id, tunnel_id, type, domain, path_prefix, tls_mode, listen_addr, target_addr, enabled, created_at
		FROM services WHERE tunnel_id = ? ORDER BY created_at
	`
	
	rows, err := s.db.Query(query, tunnelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []*models.Service
	for rows.Next() {
		var service models.Service
		err := rows.Scan(
			&service.ID, &service.TunnelID, &service.Type, &service.Domain, &service.PathPrefix,
			&service.TLSMode, &service.ListenAddr, &service.TargetAddr, &service.Enabled, &service.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		services = append(services, &service)
	}

	return services, rows.Err()
}

// UpdateService updates a service
func (s *Store) UpdateService(service *models.Service) error {
	query := `
		UPDATE services 
		SET domain = ?, path_prefix = ?, tls_mode = ?, listen_addr = ?, target_addr = ?, enabled = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query,
		service.Domain, service.PathPrefix, service.TLSMode,
		service.ListenAddr, service.TargetAddr, service.Enabled,
		service.ID,
	)
	return err
}

// DeleteService deletes a service
func (s *Store) DeleteService(id string) error {
	query := `DELETE FROM services WHERE id = ?`
	_, err := s.db.Exec(query, id)
	return err
}

// Check operations

// CreateCheck creates a new uptime check
func (s *Store) CreateCheck(check *models.Check) error {
	query := `
		INSERT INTO checks (tunnel_id, status, latency_ms, error, created_at)
		VALUES (?, ?, ?, ?, ?)
	`
	_, err := s.db.Exec(query, check.TunnelID, check.Status, check.LatencyMs, check.Error, check.CreatedAt)
	return err
}

// GetChecks gets recent checks for a tunnel
func (s *Store) GetChecks(tunnelID string, limit int) ([]*models.Check, error) {
	query := `
		SELECT id, tunnel_id, status, latency_ms, error, created_at
		FROM checks WHERE tunnel_id = ?
		ORDER BY created_at DESC LIMIT ?
	`
	
	rows, err := s.db.Query(query, tunnelID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var checks []*models.Check
	for rows.Next() {
		var check models.Check
		err := rows.Scan(
			&check.ID, &check.TunnelID, &check.Status, &check.LatencyMs, &check.Error, &check.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		checks = append(checks, &check)
	}

	return checks, rows.Err()
}

// GetUptimeStats calculates uptime statistics for a tunnel
func (s *Store) GetUptimeStats(tunnelID string) (*models.UptimeStats, error) {
	now := time.Now()
	
	stats := &models.UptimeStats{}
	
	// 24h uptime
	uptime24h, err := s.calculateUptime(tunnelID, now.Add(-24*time.Hour), now)
	if err != nil {
		return nil, err
	}
	stats.Uptime24h = uptime24h
	
	// 7d uptime
	uptime7d, err := s.calculateUptime(tunnelID, now.Add(-7*24*time.Hour), now)
	if err != nil {
		return nil, err
	}
	stats.Uptime7d = uptime7d
	
	// 30d uptime
	uptime30d, err := s.calculateUptime(tunnelID, now.Add(-30*24*time.Hour), now)
	if err != nil {
		return nil, err
	}
	stats.Uptime30d = uptime30d
	
	return stats, nil
}

// calculateUptime calculates uptime percentage for a time range
func (s *Store) calculateUptime(tunnelID string, start, end time.Time) (float64, error) {
	query := `
		SELECT COUNT(*) as total, 
		       COUNT(CASE WHEN status = 'up' THEN 1 END) as up_count
		FROM checks 
		WHERE tunnel_id = ? AND created_at BETWEEN ? AND ?
	`
	
	var total, upCount int
	err := s.db.QueryRow(query, tunnelID, start, end).Scan(&total, &upCount)
	if err != nil {
		return 0, err
	}
	
	if total == 0 {
		return 0, nil
	}
	
	return float64(upCount) / float64(total) * 100, nil
}

// CleanupOldChecks removes checks older than the specified duration
func (s *Store) CleanupOldChecks(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)
	query := `DELETE FROM checks WHERE created_at < ?`
	_, err := s.db.Exec(query, cutoff)
	return err
}