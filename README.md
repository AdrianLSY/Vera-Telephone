# Telephone

**A lightweight WebSocket-based reverse proxy sidecar for the Vera Reverse Proxy**

[![Go Version](https://img.shields.io/badge/go-1.23-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

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

---

## Quick Start

### Prerequisites

- Go 1.23+
- Plugboard server running (default: `localhost:4000`)
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

Create a `.env` file:

```bash
TELEPHONE_TOKEN={Generate one from plugboard}
SECRET_KEY_BASE={Generate one via `openssl rand -hex 32`}
PLUGBOARD_URL=ws://localhost:4000/telephone/websocket
BACKEND_HOST=localhost
BACKEND_PORT=3000
CONNECT_TIMEOUT=10s
REQUEST_TIMEOUT=30s
HEARTBEAT_INTERVAL=30s
INITIAL_BACKOFF=1s
MAX_BACKOFF=30s
MAX_RETRIES=-1
TOKEN_DB_PATH=./telephone.db
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
┌─────────────┐
│   Client    │
└──────┬──────┘
       │ HTTP Request: GET /api/users
       ▼
┌─────────────────────────────────┐
│      Plugboard (:4000)          │
│  Matches /api → Telephone       │
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

- ✅ **Survives Restarts**: No need to regenerate tokens after restart
- ✅ **Secure**: Tokens encrypted at rest with AES-256-GCM
- ✅ **Automatic Cleanup**: Expired tokens automatically removed
- ✅ **Seamless**: Works transparently in the background

### Security

- Tokens are encrypted using your `SECRET_KEY_BASE`
- Uses industry-standard AES-256-GCM authenticated encryption
- Database file (`telephone.db`) contains only encrypted data
- **Keep your `SECRET_KEY_BASE` secret!**

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
  -e token=$TELEPHONE_TOKEN \
  -e BACKEND_HOST=your-app \
  -e BACKEND_PORT=8080 \
  verastack/telephone:latest
```

### Docker Compose

```yaml
version: '3.8'

services:
  app:
    image: your-app:latest
    ports:
      - "8080:8080"

  telephone:
    image: verastack/telephone:latest
    environment:
      - token=${TELEPHONE_TOKEN}
      - BACKEND_HOST=app
      - BACKEND_PORT=8080
    depends_on:
      - app
```

---

## Configuration Reference

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `token` or `TELEPHONE_TOKEN` | JWT token from Plugboard | - | ✅ (or loaded from DB) |
| `SECRET_KEY_BASE` | Secret key for encrypting tokens (64 char hex) | - | ✅ |
| `CONNECT_TIMEOUT` | Connection timeout | - | ✅ |
| `REQUEST_TIMEOUT` | Request timeout | - | ✅ |
| `HEARTBEAT_INTERVAL` | WebSocket heartbeat interval | - | ✅ |
| `INITIAL_BACKOFF` | Initial reconnection backoff | - | ✅ |
| `MAX_BACKOFF` | Maximum reconnection backoff | - | ✅ |
| `MAX_RETRIES` | Max reconnection retries (-1 = infinite) | - | ✅ |
| `PLUGBOARD_URL` | WebSocket URL to Plugboard | `ws://localhost:4000/telephone/websocket` | ❌ |
| `BACKEND_HOST` | Backend hostname | `localhost` | ❌ |
| `BACKEND_PORT` | Backend port | `8080` | ❌ |
| `TOKEN_DB_PATH` | Path to SQLite token database | `./telephone.db` | ❌ |

---

## Features

### ✅ Connection & Authentication
- WebSocket connection with automatic reconnection
- Configurable exponential backoff and retry limits
- JWT token parsing and validation
- Automatic token refresh at half-life (dynamic based on token lifespan)
- **Encrypted token persistence** - Refreshed tokens survive restarts
  - AES-256-GCM encryption
  - SQLite database storage
  - Automatic cleanup of expired tokens

### ✅ Proxy Engine
- HTTP request forwarding (GET, POST, PUT, PATCH, DELETE)
- Request body support
- Header and query parameter forwarding
- Configurable request timeouts
- Concurrent request handling

### ✅ Response Handling
- Standard responses
- Chunked responses (auto-chunking >1MB)
- Streaming support
- Error responses

### ✅ Resilience
- Connection monitoring (every 5s)
- Automatic reconnection on disconnect
- Graceful shutdown (waits up to 30s for active requests)
- Request correlation with UUIDs

### ✅ Monitoring
- Connection status reporting
- Token expiry tracking
- Structured logging

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

## Development

### Project Structure

```
Telephone/
├── cmd/telephone/       # CLI application
├── pkg/
│   ├── auth/           # JWT authentication
│   ├── channels/       # Phoenix Channels protocol
│   ├── config/         # Configuration management
│   └── proxy/          # Main proxy engine
├── test_server/        # Test HTTP server
├── Dockerfile          # Docker build
├── Makefile           # Build automation
└── README.md          # This file
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

### Key Events

- **phx_join** - Join the telephone channel
- **heartbeat** - Keep connection alive (every 30s)
- **heartbeat_ack** - Heartbeat acknowledgment
- **proxy_req** - Incoming HTTP request from Plugboard
- **proxy_res** - Response to Plugboard
- **refresh_token** - Request new JWT token

### Example Proxy Request

```json
[null, null, "telephone:path-id", "proxy_req", {
  "request_id": "uuid",
  "method": "GET",
  "path": "/api/users",
  "headers": {"host": "example.com"},
  "query_string": "page=1",
  "body": ""
}]
```

### Example Proxy Response

```json
[null, "ref", "telephone:path-id", "proxy_res", {
  "request_id": "uuid",
  "status": 200,
  "headers": {"content-type": "application/json"},
  "body": "{\"users\":[]}",
  "chunked": false
}]
```

---

## Troubleshooting

### Connection Refused

**Problem**: `websocket: bad handshake`

**Solution**: Ensure Plugboard is running on port 4000 and the WebSocket endpoint is accessible.

### Token Expired

**Problem**: `token is expired`

**Solution**: Generate a new token from Plugboard.

### Backend Unreachable

**Problem**: `backend request failed: connection refused`

**Solution**: Ensure your backend is running on the configured port (default: 8080).

### Wrong Path

**Problem**: Requests return 404

**Solution**: Verify the path in Plugboard. If your token is for `/test`, requests should go to `/call/test/*`.

---

## Performance

- **Startup Time**: <1 second
- **Request Latency**: <10ms (local)
- **Memory Usage**: ~15MB base
- **Binary Size**: ~8MB (optimized)
- **Docker Image**: ~20MB (Alpine-based)

---

## Roadmap

### Future Enhancements
- [ ] Unit test suite
- [ ] Prometheus metrics
- [ ] Distributed tracing (OpenTelemetry)
- [ ] Request/response caching
- [ ] Circuit breaker pattern
- [ ] Helm chart for Kubernetes
- [ ] CI/CD pipeline

---

## License

Licensed under the **MIT License**. See [LICENSE](../LICENSE) for details.

---

## Related Projects

- **[Plugboard](../Plugboard/)** - The reverse proxy server
- **[Vera-Stack](../)** - Complete system documentation

---

## Support

For issues, questions, or contributions, please refer to the main Vera-Stack repository.

---
