package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jclement/picotunnel/internal/tunnel"
)

// Client represents a tunnel client
type Client struct {
	serverAddr string
	token      string
	insecure   bool
	conn       *tunnel.Connection
	streamMgr  *tunnel.StreamManager
	forwarder  *Forwarder
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

// Config holds client configuration
type Config struct {
	ServerAddr string
	Token      string
	Insecure   bool
}

// NewClient creates a new tunnel client
func NewClient(config Config) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		serverAddr: config.ServerAddr,
		token:      config.Token,
		insecure:   config.Insecure,
		ctx:        ctx,
		cancel:     cancel,
		forwarder:  NewForwarder(),
	}
}

// Start starts the client
func (c *Client) Start() error {
	log.Printf("Starting tunnel client, connecting to %s", c.serverAddr)

	// Start with initial connection
	if err := c.connect(); err != nil {
		return fmt.Errorf("initial connection failed: %w", err)
	}

	// Start reconnect loop
	c.wg.Add(1)
	go c.reconnectLoop()

	return nil
}

// Stop stops the client
func (c *Client) Stop() error {
	log.Printf("Stopping tunnel client")
	
	c.cancel()
	
	c.mu.Lock()
	if c.streamMgr != nil {
		c.streamMgr.Stop()
	}
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()

	c.wg.Wait()
	return nil
}

// connect establishes a connection to the server
func (c *Client) connect() error {
	serverURL := fmt.Sprintf("wss://%s/tunnel?token=%s", c.serverAddr, c.token)
	if c.insecure {
		serverURL = fmt.Sprintf("ws://%s/tunnel?token=%s", c.serverAddr, c.token)
	}

	_, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = tunnel.HandshakeTimeout
	
	if c.insecure {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	header := http.Header{}
	header.Set("User-Agent", "picotunnel-client/1.0")

	log.Printf("Connecting to %s", serverURL)
	ws, _, err := dialer.Dial(serverURL, header)
	if err != nil {
		return fmt.Errorf("WebSocket dial failed: %w", err)
	}

	log.Printf("WebSocket connected, establishing tunnel")

	// Create tunnel connection
	conn, err := tunnel.NewConnection(ws, c.token, false)
	if err != nil {
		ws.Close()
		return fmt.Errorf("failed to create tunnel connection: %w", err)
	}

	// Create stream manager
	streamMgr := tunnel.NewStreamManager(conn)
	streamMgr.RegisterHandler("http", c.forwarder)
	streamMgr.RegisterHandler("tcp", c.forwarder)

	c.mu.Lock()
	c.conn = conn
	c.streamMgr = streamMgr
	c.mu.Unlock()

	// Start stream manager
	if err := streamMgr.Start(); err != nil {
		conn.Close()
		return fmt.Errorf("failed to start stream manager: %w", err)
	}

	// Start control message handler
	c.wg.Add(1)
	go c.handleControlMessages()

	log.Printf("Tunnel established successfully")
	return nil
}

// handleControlMessages handles control messages from the server
func (c *Client) handleControlMessages() {
	defer c.wg.Done()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil || conn.IsClosed() {
			return
		}

		msg, err := conn.ReadMessage()
		if err != nil {
			if c.ctx.Err() == nil {
				log.Printf("Error reading control message: %v", err)
			}
			return
		}

		switch msg.Type {
		case "ping":
			if err := conn.Pong(); err != nil {
				log.Printf("Failed to send pong: %v", err)
				return
			}
			conn.UpdateLastPing()
		case "pong":
			conn.UpdateLastPing()
		default:
			log.Printf("Unknown control message type: %s", msg.Type)
		}
	}
}

// reconnectLoop handles automatic reconnection with exponential backoff
func (c *Client) reconnectLoop() {
	defer c.wg.Done()

	backoff := time.Second
	maxBackoff := time.Minute * 5

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Check if connection is alive
		c.mu.RLock()
		conn := c.conn
		needReconnect := conn == nil || conn.IsClosed()
		c.mu.RUnlock()

		if needReconnect {
			log.Printf("Connection lost, reconnecting in %v", backoff)
			
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(backoff):
			}

			if err := c.connect(); err != nil {
				log.Printf("Reconnection failed: %v", err)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}

			backoff = time.Second // Reset backoff on successful connection
		} else {
			// Check connection health
			if time.Since(conn.LastPing()) > tunnel.PingInterval*3 {
				log.Printf("Connection appears stale, forcing reconnect")
				c.mu.Lock()
				if c.streamMgr != nil {
					c.streamMgr.Stop()
				}
				if c.conn != nil {
					c.conn.Close()
				}
				c.conn = nil
				c.streamMgr = nil
				c.mu.Unlock()
				continue
			}
		}

		// Sleep before next check
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(time.Second * 10):
		}
	}
}