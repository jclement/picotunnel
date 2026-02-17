# PicoTunnel

A self-hosted tunnel proxy that lets you expose local services through a central server without relying on Cloudflare Tunnels. Think of it as your own ngrok or Cloudflare Tunnel alternative.

## Features

- **HTTP Proxying**: Route traffic based on domain names with optional TLS termination
- **TCP Proxying**: Forward raw TCP connections to local services  
- **Web Management UI**: Clean, modern interface for managing tunnels and services
- **Uptime Monitoring**: PicoStatus-style uptime tracking and statistics
- **OIDC Authentication**: Secure your management UI with any OIDC provider
- **Let's Encrypt**: Automatic TLS certificate management
- **Docker Ready**: Minimal Docker images for easy deployment
- **Multi-platform**: Runs on Linux, macOS, Windows (arm64 + amd64)

## Quick Start

### 1. Run the Server

```bash
# Using Docker (recommended)
docker run -d --name picotunnel-server \
  -p 8080:8080 \
  -p 8443:8443 \
  -p 80:80 \
  -p 443:443 \
  -v picotunnel-data:/data \
  ghcr.io/jclement/picotunnel-server

# Or download binary from releases
./picotunnel-server --data ./data
```

### 2. Create a Tunnel

Visit http://your-server:8080 to access the web UI, or use the API:

```bash
curl -X POST http://your-server:8080/api/tunnels \
  -H "Content-Type: application/json" \
  -d '{"name": "my-app"}'
```

This returns a tunnel token that you'll use with the client.

### 3. Run the Client

```bash  
# Using Docker
docker run -d --name picotunnel-client \
  ghcr.io/jclement/picotunnel-client \
  --server your-server.com:8443 \
  --token YOUR_TUNNEL_TOKEN

# Or download binary
./picotunnel-client --server your-server.com:8443 --token YOUR_TUNNEL_TOKEN
```

### 4. Configure Services

Add HTTP or TCP services through the web UI or API:

```bash
# HTTP service
curl -X POST http://your-server:8080/api/tunnels/TUNNEL_ID/services \
  -H "Content-Type: application/json" \
  -d '{
    "type": "http",
    "domain": "myapp.example.com", 
    "target_addr": "localhost:8080",
    "enabled": true
  }'

# TCP service  
curl -X POST http://your-server:8080/api/tunnels/TUNNEL_ID/services \
  -H "Content-Type: application/json" \
  -d '{
    "type": "tcp",
    "listen_addr": "0.0.0.0:2222",
    "target_addr": "localhost:22", 
    "enabled": true
  }'
```

## Architecture

### Server Components

- **Management UI**: Web interface on port 8080 for managing tunnels
- **Tunnel Endpoint**: WebSocket server on port 8443 for client connections  
- **HTTP Proxy**: Listens on port 80/443 and routes by domain
- **TCP Proxy**: Dynamic listeners for each TCP service
- **SQLite Database**: Stores configuration and uptime history

### Client Components

- **Tunnel Client**: Maintains persistent WebSocket connection to server
- **Stream Forwarder**: Forwards individual requests to local services
- **Auto-reconnect**: Handles connection failures with exponential backoff

### Protocol

1. Client connects via WebSocket: `wss://server:8443/tunnel?token=xxx`
2. [yamux](https://github.com/hashicorp/yamux) multiplexes streams over the WebSocket
3. Server opens new stream for each incoming request (HTTP/TCP)
4. Client forwards stream to local target service
5. Bidirectional byte copying until stream closes

## Configuration

### Server Environment Variables

```bash
PICOTUNNEL_LISTEN_ADDR=:8080          # Management UI + API
PICOTUNNEL_TUNNEL_ADDR=:8443          # Tunnel client connections (WSS)  
PICOTUNNEL_HTTP_ADDR=:80              # HTTP proxy listener
PICOTUNNEL_HTTPS_ADDR=:443            # HTTPS proxy listener
PICOTUNNEL_DATA_DIR=/data             # SQLite DB + certificates
PICOTUNNEL_DOMAIN=tunnel.example.com  # Server domain

# OIDC Authentication (optional)
PICOTUNNEL_OIDC_ISSUER=https://auth.example.com
PICOTUNNEL_OIDC_CLIENT_ID=picotunnel
PICOTUNNEL_OIDC_CLIENT_SECRET=secret
PICOTUNNEL_OIDC_REDIRECT_URL=https://tunnel.example.com/auth/callback

# Let's Encrypt (optional)
PICOTUNNEL_ACME_ENABLED=true
PICOTUNNEL_ACME_EMAIL=admin@example.com
```

### Client Environment Variables

```bash
PICOTUNNEL_SERVER=tunnel.example.com:8443  # Server address
PICOTUNNEL_TOKEN=your-tunnel-token         # Auth token
PICOTUNNEL_INSECURE=false                  # Skip TLS verify (dev only)
```

## Development

### Prerequisites

- Go 1.23+
- Node.js 22+ (for web UI)
- Docker (for containers)

### Setup

```bash
git clone https://github.com/jclement/picotunnel.git
cd picotunnel
make setup
```

### Build

```bash
# Build everything
make build

# Build server only
make build-server

# Build client only  
make build-client

# Build Docker images
make docker-build
```

### Run Development Server

```bash
make dev-server
```

This starts the server with:
- Management UI on http://localhost:8080
- Tunnel endpoint on ws://localhost:8443  
- Data stored in `./dev-data/`

## Web UI

The web interface provides:

- **Dashboard**: List of tunnels with connection status and uptime
- **Tunnel Details**: Services, configuration, uptime stats, recent checks  
- **Service Management**: Add/edit/delete HTTP and TCP services
- **Docker Commands**: Copy-paste client setup commands
- **Uptime Graphs**: PicoStatus-style heartbeat visualization

## API Reference

### Tunnels

```bash
GET    /api/tunnels                 # List all tunnels
POST   /api/tunnels                 # Create tunnel  
GET    /api/tunnels/:id             # Get tunnel details
PATCH  /api/tunnels/:id             # Update tunnel
DELETE /api/tunnels/:id             # Delete tunnel
POST   /api/tunnels/:id/regenerate  # Regenerate token
```

### Services

```bash
GET    /api/tunnels/:id/services    # List services
POST   /api/tunnels/:id/services    # Create service
PATCH  /api/services/:id            # Update service  
DELETE /api/services/:id            # Delete service
```

### Monitoring

```bash
GET    /api/tunnels/:id/checks      # Get uptime history
GET    /api/tunnels/:id/stats       # Get uptime statistics
GET    /health                      # Server health check
```

## Deployment

### Docker Compose

```yaml
version: '3.8'
services:
  picotunnel:
    image: ghcr.io/jclement/picotunnel-server:latest
    ports:
      - "8080:8080"   # Management UI
      - "8443:8443"   # Tunnel endpoint  
      - "80:80"       # HTTP proxy
      - "443:443"     # HTTPS proxy
    volumes:
      - picotunnel-data:/data
    environment:
      PICOTUNNEL_DOMAIN: tunnel.example.com
      PICOTUNNEL_ACME_ENABLED: "true"
      PICOTUNNEL_ACME_EMAIL: admin@example.com
    restart: unless-stopped

volumes:
  picotunnel-data:
```

### Kubernetes

See `examples/kubernetes/` for YAML manifests.

### systemd Service

```ini
[Unit]
Description=PicoTunnel Server
After=network.target

[Service]  
Type=simple
User=picotunnel
ExecStart=/usr/local/bin/picotunnel-server --data /var/lib/picotunnel
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

## Comparison

| Feature | PicoTunnel | Cloudflare Tunnel | ngrok | Telebit |
|---------|------------|------------------|-------|---------|
| **Self-hosted** | ✅ | ❌ | ❌ | ✅ |
| **HTTP proxying** | ✅ | ✅ | ✅ | ✅ |
| **TCP proxying** | ✅ | ✅ | ✅ | ✅ |  
| **Custom domains** | ✅ | ✅ | ❌ (paid) | ✅ |
| **Web UI** | ✅ | ✅ | ✅ | ❌ |
| **OIDC auth** | ✅ | ✅ | ❌ | ❌ |
| **Let's Encrypt** | ✅ | ✅ | ❌ | ✅ |
| **Uptime monitoring** | ✅ | ✅ | ❌ | ❌ |

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes and add tests  
4. Run `make test` and `make vet`
5. Submit a pull request

## License

MIT License - see [LICENSE](LICENSE) for details.

## Roadmap

- [x] Core tunnel protocol (WebSocket + yamux)
- [x] HTTP and TCP proxying  
- [x] SQLite storage and API
- [x] Basic web UI placeholder
- [ ] React web UI with full functionality
- [ ] Let's Encrypt integration
- [ ] OIDC authentication  
- [ ] Metrics and monitoring
- [ ] Load balancing
- [ ] Rate limiting
- [ ] Access control lists

## Support

- 📖 [Documentation](https://github.com/jclement/picotunnel/wiki)
- 🐛 [Issue Tracker](https://github.com/jclement/picotunnel/issues)  
- 💬 [Discussions](https://github.com/jclement/picotunnel/discussions)