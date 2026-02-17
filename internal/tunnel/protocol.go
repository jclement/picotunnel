package tunnel

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
	"github.com/jclement/picotunnel/internal/models"
)

// Protocol constants
const (
	PingInterval    = 30 * time.Second
	WriteTimeout    = 10 * time.Second
	ReadTimeout     = 60 * time.Second
	HandshakeTimeout = 10 * time.Second
)

// Connection wraps a WebSocket connection with yamux multiplexing
type Connection struct {
	ws       *websocket.Conn
	session  *yamux.Session
	token    string
	mu       sync.RWMutex
	closed   bool
	lastPing time.Time
}

// NewConnection creates a new tunnel connection
func NewConnection(ws *websocket.Conn, token string, isServer bool) (*Connection, error) {
	ws.SetReadDeadline(time.Now().Add(ReadTimeout))
	ws.SetWriteDeadline(time.Now().Add(WriteTimeout))

	conn := &Connection{
		ws:       ws,
		token:    token,
		lastPing: time.Now(),
	}

	// Set up yamux session
	var session *yamux.Session
	var err error

	if isServer {
		session, err = yamux.Server(NewWebSocketConn(ws), nil)
	} else {
		session, err = yamux.Client(NewWebSocketConn(ws), nil)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create yamux session: %w", err)
	}

	conn.session = session
	return conn, nil
}

// Accept accepts a new stream (server side)
func (c *Connection) Accept() (net.Conn, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, fmt.Errorf("connection closed")
	}

	return c.session.Accept()
}

// Open opens a new stream (client side)
func (c *Connection) Open() (net.Conn, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, fmt.Errorf("connection closed")
	}

	return c.session.Open()
}

// SendMessage sends a control message
func (c *Connection) SendMessage(msg models.TunnelMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("connection closed")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.ws.SetWriteDeadline(time.Now().Add(WriteTimeout))
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

// ReadMessage reads a control message
func (c *Connection) ReadMessage() (*models.TunnelMessage, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return nil, fmt.Errorf("connection closed")
	}

	c.ws.SetReadDeadline(time.Now().Add(ReadTimeout))
	_, data, err := c.ws.ReadMessage()
	if err != nil {
		return nil, err
	}

	var msg models.TunnelMessage
	err = json.Unmarshal(data, &msg)
	return &msg, err
}

// Ping sends a ping message
func (c *Connection) Ping() error {
	return c.SendMessage(models.TunnelMessage{Type: "ping"})
}

// Pong sends a pong message
func (c *Connection) Pong() error {
	return c.SendMessage(models.TunnelMessage{Type: "pong"})
}

// UpdateLastPing updates the last ping time
func (c *Connection) UpdateLastPing() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastPing = time.Now()
}

// LastPing returns the last ping time
func (c *Connection) LastPing() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastPing
}

// Token returns the connection token
func (c *Connection) Token() string {
	return c.token
}

// Close closes the connection
func (c *Connection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	if c.session != nil {
		c.session.Close()
	}

	return c.ws.Close()
}

// IsClosed returns whether the connection is closed
func (c *Connection) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// WebSocketConn wraps a WebSocket connection to implement net.Conn interface
type WebSocketConn struct {
	ws       *websocket.Conn
	reader   io.Reader
	readBuf  []byte
	readMu   sync.Mutex
	writeMu  sync.Mutex
}

// NewWebSocketConn creates a new WebSocket connection wrapper
func NewWebSocketConn(ws *websocket.Conn) *WebSocketConn {
	return &WebSocketConn{ws: ws}
}

// Read implements io.Reader
func (w *WebSocketConn) Read(b []byte) (int, error) {
	w.readMu.Lock()
	defer w.readMu.Unlock()

	if w.reader == nil {
		_, r, err := w.ws.NextReader()
		if err != nil {
			return 0, err
		}
		w.reader = r
	}

	n, err := w.reader.Read(b)
	if err == io.EOF {
		w.reader = nil
	}
	return n, err
}

// Write implements io.Writer
func (w *WebSocketConn) Write(b []byte) (int, error) {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()

	writer, err := w.ws.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	defer writer.Close()

	return writer.Write(b)
}

// Close implements io.Closer
func (w *WebSocketConn) Close() error {
	return w.ws.Close()
}

// LocalAddr implements net.Conn
func (w *WebSocketConn) LocalAddr() net.Addr {
	return w.ws.LocalAddr()
}

// RemoteAddr implements net.Conn
func (w *WebSocketConn) RemoteAddr() net.Addr {
	return w.ws.RemoteAddr()
}

// SetDeadline implements net.Conn
func (w *WebSocketConn) SetDeadline(t time.Time) error {
	if err := w.SetReadDeadline(t); err != nil {
		return err
	}
	return w.SetWriteDeadline(t)
}

// SetReadDeadline implements net.Conn
func (w *WebSocketConn) SetReadDeadline(t time.Time) error {
	return w.ws.SetReadDeadline(t)
}

// SetWriteDeadline implements net.Conn
func (w *WebSocketConn) SetWriteDeadline(t time.Time) error {
	return w.ws.SetWriteDeadline(t)
}

// Upgrader for WebSocket connections
var Upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow connections from any origin
	},
	Subprotocols: []string{"tunnel"},
}