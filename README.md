# Vera Telephone

**A lightweight WebSocket-based reverse proxy sidecar for the Vera Reverse Proxy**

[![Go Version](https://img.shields.io/badge/go-1.24-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![CI](https://github.com/AdrianLSY/Vera-Telephone/actions/workflows/ci.yml/badge.svg)](https://github.com/AdrianLSY/Vera-Telephone/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/AdrianLSY/Vera-Telephone/branch/main/graph/badge.svg)](https://codecov.io/gh/AdrianLSY/Vera-Telephone)

---

## Overview

Telephone is a sidecar process that maintains a persistent WebSocket connection to the Plugboard reverse proxy. It enables dynamic routing by forwarding HTTP requests from Plugboard to your local application through a secure WebSocket tunnel.

### Key Features

- **WebSocket Tunnel** - Persistent connection with automatic reconnection
- **Phoenix Channels Protocol** - Full implementation of Elixir Phoenix channels
- **JWT Authentication** - Token-based auth with automatic refresh at half-life
- **Graceful Shutdown** - Waits for active requests before stopping
- **Chunked Responses** - Automatic chunking for large responses (>1MB)
- **Fully Configurable** - All timeouts, backoff, and retry settings via environment variables
- **Token Persistence** - Encrypted token storage with automatic refresh
- **WebSocket Proxy** - Bidirectional WebSocket proxying to backend services

---

## Quick Start

### Prerequisites

- Go 1.24+
- Plugboard server running (e.g., `localhost:4000`)
- JWT token from Plugboard

### Installation

```bash
# Clone the repository
cd Telephone

# Build
make build

# Or build directly
go build -o bin/telephone ./cmd/telephone
```

### Configuration

**All configuration must be explicitly set - no defaults are used.**

Create a `.env` file with all required variables:

```bash
# Authentication
TELEPHONE_TOKEN={Generate one from plugboard}
SECRET_KEY_BASE={Generate one via `openssl rand -hex 32`}

# Connection
PLUGBOARD_URL=ws://localhost:4000/telephone/websocket
BACKEND_HOST=localhost
BACKEND_PORT=8080
BACKEND_SCHEME=http

# Timeouts
CONNECT_TIMEOUT=10s
REQUEST_TIMEOUT=30s
HEARTBEAT_INTERVAL=30s
CONNECTION_MONITOR_INTERVAL=5s

# Reconnection
INITIAL_BACKOFF=1s
MAX_BACKOFF=30s
MAX_RETRIES=-1

# Storage & Limits
TOKEN_DB_PATH=./telephone.db
MAX_RESPONSE_SIZE=104857600
CHUNK_SIZE=1048576
DB_TIMEOUT=10s
```

### Run

```bash
# Start your backend application on port 8080
# Then start Telephone
./bin/telephone
```

### Test

```bash
# Make a request through Plugboard
curl http://localhost:4000/call/YOUR_PATH/
```

---

## How It Works

```
┌──────────────┐
│    Client    │
└──────┬───────┘
       │ HTTP Request: GET /call/xyz
       ▼
┌─────────────────────────────────┐
│        Plugboard (:4000)        │
│  Matches /call/xyz → Telephone  │
└────────────┬────────────────────┘
             │ WebSocket Message
             ▼
┌─────────────────────────────────┐
│      Telephone (Go)             │
│  • Receives proxy request       │
│  • Forwards to localhost:8080   │
│  • Returns response             │
└────────────┬────────────────────┘
             │ HTTP
             ▼
┌─────────────────────────────────┐
│   Your Application (:8080)      │
└─────────────────────────────────┘
```

---

## Token Persistence

Telephone **automatically saves refreshed tokens** to an encrypted SQLite database, ensuring they survive restarts.

### How It Works

1. **Initial Start**: Uses token from environment variable
2. **Token Refresh**: Automatically refreshes token at half of its lifespan (e.g., for a 1-hour token, refreshes after 30 minutes)
3. **Encryption**: Encrypts token with AES-256-GCM using `SECRET_KEY_BASE`
4. **Storage**: Saves encrypted token to SQLite database
5. **On Restart**: Loads most recent valid token from database
6. **Fallback**: If no valid DB token, uses environment token

### Benefits

- **Survives Restarts**: No need to regenerate tokens after restart
- **Secure**: Tokens encrypted at rest with AES-256-GCM
- **Automatic Cleanup**: Expired tokens automatically removed
- **Seamless**: Works transparently in the background

### Security

- Tokens are encrypted using your `SECRET_KEY_BASE`
- Uses industry-standard AES-256-GCM authenticated encryption
- Database file (`telephone.db`) contains only encrypted data
- **Keep your `SECRET_KEY_BASE` secret!**
- Tokens are sent as query parameters (required by Phoenix Socket)
- Token leakage is mitigated via:
  - Short-lived tokens with automatic refresh
  - Clean URL logging (tokens stripped from logs)
  - TLS in production (`wss://` encrypts query params in transit)
  - No browser involvement (server-to-server communication)

### Token Security Considerations

**Why query parameters?**
Phoenix Socket requires authentication tokens in query parameters, not HTTP headers. This is the standard Phoenix authentication pattern.

**Security mitigations:**
- **TLS in production**: Use `wss://` URLs to encrypt all communication including query parameters
- **Short-lived tokens**: Tokens automatically refresh at half-life (e.g., 30-minute refresh for 1-hour tokens)
- **Clean logging**: All logs use `GetCleanURL()` which strips tokens from URLs
- **Server-to-server**: No browser caching, history, or referrer issues
- **Encrypted storage**: Tokens encrypted at rest with AES-256-GCM

**Production checklist:**
- [ ] Use `wss://` (TLS) for PLUGBOARD_URL
- [ ] Configure log sanitization to filter query parameters if additional security needed
- [ ] Rotate SECRET_KEY_BASE periodically
- [ ] Monitor token refresh logs for anomalies

### Database Location

Default: `./telephone.db` (current directory)

Change via environment variable:
```bash
TOKEN_DB_PATH=/var/lib/telephone/tokens.db
```

---

## Docker

### Build Image

```bash
make docker-build
```

### Run Container

```bash
docker run --rm -it \
  -e TELEPHONE_TOKEN=$TELEPHONE_TOKEN \
  -e SECRET_KEY_BASE=$SECRET_KEY_BASE \
  -e PLUGBOARD_URL=ws://plugboard:4000/telephone/websocket \
  -e BACKEND_HOST=your-app \
  -e BACKEND_PORT=8080 \
  -e BACKEND_SCHEME=http \
  -e CONNECT_TIMEOUT=10s \
  -e REQUEST_TIMEOUT=30s \
  -e HEARTBEAT_INTERVAL=30s \
  -e CONNECTION_MONITOR_INTERVAL=5s \
  -e INITIAL_BACKOFF=1s \
  -e MAX_BACKOFF=30s \
  -e MAX_RETRIES=-1 \
  -e TOKEN_DB_PATH=/data/telephone.db \
  -e MAX_RESPONSE_SIZE=104857600 \
  -e CHUNK_SIZE=1048576 \
  -e DB_TIMEOUT=10s \
  -v telephone-data:/data \
  verastack/telephone:latest
```

**Note**: The volume mount (`-v telephone-data:/data`) is recommended to persist the token database across container restarts.

---

## Configuration Reference

### Environment Variables

**All configuration variables are required - no defaults are provided.**

See [`.env.example`](.env.example) for a complete example configuration with detailed comments.

| Variable                      | Description                                    | Example Value                             |
|-------------------------------|------------------------------------------------|-------------------------------------------|
| `TELEPHONE_TOKEN`             | JWT token from Plugboard (optional*)           | `eyJhbGci...`                             |
| `SECRET_KEY_BASE`             | Secret key for encrypting tokens (64 char hex) | `6a5c5a634bc0c4c7...`                     |
| `PLUGBOARD_URL`               | WebSocket URL to Plugboard                     | `ws://localhost:4000/telephone/websocket` |
| `BACKEND_HOST`                | Backend hostname                               | `localhost`                               |
| `BACKEND_PORT`                | Backend port                                   | `8080`                                    |
| `BACKEND_SCHEME`              | Backend URL scheme (http or https)             | `http`                                    |
| `CONNECT_TIMEOUT`             | Connection timeout                             | `10s`                                     |
| `REQUEST_TIMEOUT`             | Request timeout                                | `30s`                                     |
| `HEARTBEAT_INTERVAL`          | WebSocket heartbeat interval                   | `30s`                                     |
| `CONNECTION_MONITOR_INTERVAL` | Connection health check interval               | `5s`                                      |
| `INITIAL_BACKOFF`             | Initial reconnection backoff                   | `1s`                                      |
| `MAX_BACKOFF`                 | Maximum reconnection backoff                   | `30s`                                     |
| `MAX_RETRIES`                 | Max reconnection retries (-1 for infinite)     | `100`                                     |
| `TOKEN_DB_PATH`               | Path to SQLite token database                  | `./telephone.db`                          |
| `MAX_RESPONSE_SIZE`           | Maximum response size in bytes                 | `104857600`                               |
| `CHUNK_SIZE`                  | Chunk size for streaming responses             | `1048576`                                 |
| `DB_TIMEOUT`                  | Database operation timeout                     | `10s`                                     |
| `HEALTH_CHECK_PORT`           | Port for health check HTTP server (optional)   | `8081`                                    |

\* `TELEPHONE_TOKEN` is optional if a valid token exists in the database from a previous run.

---

## Features

### Connection & Authentication
- WebSocket connection with automatic reconnection
- Configurable exponential backoff and retry limits
- JWT token parsing and validation
- Automatic token refresh at half-life (dynamic based on token lifespan)
- **Encrypted token persistence** - Refreshed tokens survive restarts
  - AES-256-GCM encryption
  - SQLite database storage
  - Automatic cleanup of expired tokens

### Proxy Engine
- HTTP request forwarding (GET, POST, PUT, PATCH, DELETE)
- Request body support
- Header and query parameter forwarding
- Configurable request timeouts
- Concurrent request handling
- **WebSocket proxy** - Bidirectional WebSocket connections to backend services
  - Transparent subprotocol negotiation
  - Base64-encoded frame forwarding
  - Graceful connection lifecycle management

### Response Handling
- Standard responses
- Chunked responses (auto-chunking >1MB)
- Streaming support
- Error responses

### Resilience
- Connection monitoring (every 5s)
- Automatic reconnection on disconnect
- Graceful shutdown (waits up to 30s for active requests)
- Request correlation with UUIDs

### Monitoring
- Connection status reporting
- Token expiry tracking
- Structured logging
- **Health check endpoints** (optional) - Kubernetes-compatible `/health`, `/ready`, `/live` endpoints

---

## Makefile Commands

```bash
make help          # Show all available commands
make build         # Build the binary
make build-all     # Build for all platforms
make run           # Build and run
make test          # Run tests
make lint          # Run linter
make fmt           # Format code
make clean         # Clean build artifacts
make docker-build  # Build Docker image
make docker-run    # Build and run in Docker
```

---

## Architecture

### Core Components

**Telephone** (`pkg/proxy/telephone.go`)
- Main proxy engine managing WebSocket connection lifecycle
- Handles concurrent requests via UUID-based correlation
- Implements token management with automatic refresh at half-life
- Graceful shutdown waiting for active requests to complete
- Exponential backoff reconnection with configurable limits

**ChannelsClient** (`pkg/channels/client.go`)
- Full Phoenix Channels protocol implementation over WebSocket
- Message format: `[join_ref, ref, topic, event, payload]`
- Handles channel join/leave, heartbeat, and message routing
- Thread-safe connection management with read/write separation

**JWTClaims** (`pkg/auth/jwt.go`)
- JWT token parsing and claim extraction
- Extracts path_id and expiry for connection routing
- Token validation delegated to Plugboard server

**Config** (`pkg/config/config.go`)
- Environment-based configuration loading
- Strict validation - all required values must be set
- Duration parsing for timeout/interval values

**TokenStore** (`pkg/storage/token_store.go`)
- Encrypted token persistence using SQLite
- AES-256-GCM authenticated encryption
- Key derivation via PBKDF2 (100,000 iterations)
- Automatic cleanup of expired tokens

### Request Correlation

Each proxied request receives a unique correlation ID, allowing multiple concurrent requests over a single WebSocket connection:

```go
type PendingRequest struct {
    RequestID    string
    ResponseChan chan *ProxyResponse
    CancelFunc   context.CancelFunc
}
```

---

## Development

### Project Structure

```
Telephone/
├── cmd/
│   ├── telephone/       # Main CLI application
│   └── token-check/     # Token validation utility
├── pkg/
│   ├── auth/           # JWT authentication
│   ├── channels/       # Phoenix Channels protocol client
│   ├── config/         # Configuration management
│   ├── proxy/          # Main proxy engine
│   ├── storage/        # Encrypted token persistence
│   └── websocket/      # WebSocket proxy connection manager
├── test_server/        # Test HTTP server for development
├── Dockerfile          # Multi-stage Docker build
├── Makefile            # Build automation
├── CLAUDE.md           # AI assistant guidance
├── AGENTS.md           # Development guidelines
└── README.md           # This file
```

### Building from Source

```bash
# Get dependencies
go mod download

# Build
go build -o bin/telephone ./cmd/telephone

# Run tests (when available)
go test ./...
```

### Testing

Start the test backend:

```bash
cd test_server
go run server.go
```

In another terminal, start Telephone:

```bash
./bin/telephone
```

Make test requests:

```bash
# Assuming your path is registered as /test
curl http://localhost:4000/call/test/
curl http://localhost:4000/call/test/echo?foo=bar
```

---

## Protocol Details

### Phoenix Channels Wire Format

Messages are JSON arrays: `[join_ref, ref, topic, event, payload]`

### Protocol Version

Telephone uses **Phoenix Channels V2 protocol**:

- **Wire format**: JSON arrays `[join_ref, ref, topic, event, payload]`
- **Version negotiation**: `vsn=2.0.0` query parameter in WebSocket URL
- **Serializer**: Server must support `Phoenix.Socket.V2.JSONSerializer`

The protocol version is sent as a query parameter during connection (matching the official Phoenix.js client behavior). Phoenix reads the `vsn` parameter to select the appropriate serializer:
- With `?vsn=2.0.0`: Uses V2 serializer (JSON arrays)
- Without `vsn`: Defaults to V1 serializer (JSON objects)

**Example WebSocket URL**:
```
ws://localhost:4000/telephone/websocket?token=<jwt>&vsn=2.0.0
```

### Key Events

- **phx_join** - Join the telephone channel
- **heartbeat** - Keep connection alive (every 30s)
- **heartbeat_ack** - Heartbeat acknowledgment
- **proxy_req** - Incoming HTTP request from Plugboard
- **proxy_res** - Response to Plugboard
- **refresh_token** - Request new JWT token

### WebSocket Proxy Events

- **ws_connect** - Client wants to establish WebSocket connection to backend
- **ws_connected** - Backend WebSocket connection established
- **ws_frame** - WebSocket frame (bidirectional, base64-encoded)
- **ws_close** - Client closed WebSocket connection
- **ws_closed** - Backend closed WebSocket connection
- **ws_error** - WebSocket error occurred

---

### Operational Limits

| Limit | Environment Variable | Typical Value | Notes |
|-------|---------------------|---------------|-------|
| Max Response Size | `MAX_RESPONSE_SIZE` | 100 MB | Responses exceeding this are rejected |
| Chunk Size | `CHUNK_SIZE` | 1 MB | Responses larger than this are automatically chunked |
| Connection Monitor | `CONNECTION_MONITOR_INTERVAL` | 5s | Health check frequency |
| Connection Retries | `MAX_RETRIES` | 100 or -1 | Use -1 for infinite retries |
| Heartbeat Timeout | - | 3x `HEARTBEAT_INTERVAL` | Connection considered dead if no ack |
| Database Timeout | `DB_TIMEOUT` | 10s | SQLite operation timeout |
| Backend Protocol | `BACKEND_SCHEME` | http/https | Supports both protocols |
| Health Check Port | `HEALTH_CHECK_PORT` | 8081 | Optional - set to enable health endpoints |

**Note:** All configuration must be explicitly set via environment variables - no defaults are provided.

---

## Health Check Endpoints

When `HEALTH_CHECK_PORT` is set, Telephone exposes HTTP health check endpoints for Kubernetes and other orchestration systems:

| Endpoint | Description | Success | Failure |
|----------|-------------|---------|---------|
| `/health` or `/healthz` | Overall health status (JSON) | 200 OK | 503 Service Unavailable |
| `/ready` or `/readyz` | Readiness probe (connected + valid token) | 200 OK | 503 Service Unavailable |
| `/live` or `/livez` | Liveness probe (process running) | 200 OK | - |

### Health Response Format

```json
{
  "status": "healthy",
  "connected": true,
  "uptime": "1h30m45s",
  "last_heartbeat": "5s ago",
  "token_expiry": "45m30s",
  "version": "1.0.0"
}
```

### Kubernetes Example

```yaml
livenessProbe:
  httpGet:
    path: /livez
    port: 8081
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /readyz
    port: 8081
  initialDelaySeconds: 5
  periodSeconds: 5
```

## License

Licensed under the **MIT License**. See [LICENSE](LICENSE) for details.

---

## Related Projects

- **[Vera Plugboard](https://github.com/AdrianLSY/Vera-Plugboard)** - The reverse proxy webserver component
- **[Vera Reverse Proxy](https://github.com/AdrianLSY/Vera-Reverse-Proxy)** - The Technology Stack

---

## Support

For issues, questions, or contributions, please refer to the main Vera-Stack repository.

---
