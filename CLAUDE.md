# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Important: Read AGENTS.md First

**Always read `AGENTS.md` before making changes.** It is the development reference/bible containing Go conventions, concurrency patterns, error handling guidelines, testing methodology, and coding standards for this project.

## Project Overview

Telephone is a WebSocket-based reverse proxy sidecar for the Vera-Stack. It maintains a persistent connection to the Plugboard reverse proxy server and forwards HTTP requests to local backend services using:
- Phoenix Channels protocol over WebSocket for communication with Plugboard
- JWT-based authentication with automatic token refresh at half-life
- Encrypted token persistence using SQLite and AES-256-GCM
- Request correlation with UUIDs for concurrent request handling
- Automatic reconnection with exponential backoff
- Chunked response support for large payloads (>1MB)

## Build & Development Commands

```bash
# Build
make build                 # Build the binary to bin/telephone
make build-all             # Build for all platforms (linux, darwin, windows)

# Run
make run                   # Build and run
./bin/telephone            # Run directly

# Testing
make test                  # Run all tests
make test-race             # Run tests with race detector
go test ./pkg/...          # Run tests for specific package
go test -v ./pkg/proxy/... # Verbose output for proxy package

# Code quality
make lint                  # Run golangci-lint
make fmt                   # Format code with go fmt
make precommit             # Run fmt + lint + test (use before committing)

# Dependencies
go mod download            # Download dependencies
go mod tidy                # Clean up go.mod and go.sum

# Docker
make docker-build          # Build Docker image
make docker-run            # Build and run in Docker
```

## Architecture

### Core Components (pkg/)

- **proxy/** (`telephone.go`, `reconnect.go`) - Main proxy engine managing the WebSocket connection lifecycle, request forwarding, and token management. Handles concurrent requests via correlation IDs and implements graceful shutdown.

- **channels/** (`client.go`, `message.go`) - Phoenix Channels protocol client. Implements the wire format `[join_ref, ref, topic, event, payload]`, handles message serialization, and manages channel joins/leaves.

- **auth/** (`jwt.go`) - JWT token parsing and validation. Extracts claims (path, expiry) from Plugboard-issued tokens without signature verification (server validates).

- **config/** (`config.go`) - Configuration management loading all settings from environment variables. Enforces required variables and validates values.

- **storage/** (`token_store.go`) - Encrypted token persistence using SQLite. Tokens are encrypted with AES-256-GCM using a key derived from `SECRET_KEY_BASE` via PBKDF2.

### Command Entry Points (cmd/)

- **cmd/telephone/** - Main application entry point. Loads configuration, initializes the Telephone instance, and runs the proxy loop.

- **cmd/token-check/** - Utility to validate and inspect JWT tokens.

### Request Flow

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                              Plugboard Server                                │
│                                                                              │
│  1. Receives HTTP request at /call/*path                                     │
│  2. Matches path to registered Telephone                                     │
│  3. Sends proxy_req message via WebSocket                                    │
└─────────────────────────────────┬────────────────────────────────────────────┘
                                  │ WebSocket (Phoenix Channels)
                                  ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                           Telephone (this project)                           │
│                                                                              │
│  pkg/channels/client.go:                                                     │
│    • Receives WebSocket message                                              │
│    • Parses Phoenix Channel format [join_ref, ref, topic, event, payload]    │
│    • Routes to registered handler                                            │
│                                                                              │
│  pkg/proxy/telephone.go:                                                     │
│    • handleProxyRequest() extracts correlation_id, method, path, headers     │
│    • Builds HTTP request to backend                                          │
│    • Forwards request using http.Client                                      │
│    • Reads response (chunks if >1MB)                                         │
│    • Sends proxy_res message back via WebSocket                              │
└─────────────────────────────────┬────────────────────────────────────────────┘
                                  │ HTTP
                                  ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                         Backend Service (your app)                           │
│                                                                              │
│  Receives: GET/POST/PUT/PATCH/DELETE at http://localhost:8080/*path          │
│  Returns: HTTP response (status, headers, body)                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

### Token Refresh Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Token Lifecycle                                                        │
│                                                                         │
│  1. Startup: Load token from env (TELEPHONE_TOKEN) or database          │
│  2. Parse JWT to extract expiry time                                    │
│  3. Calculate refresh time = (expiry - now) / 2                         │
│  4. Schedule refresh timer                                              │
│  5. On refresh: Send "refresh_token" event to Plugboard                 │
│  6. Receive new token, encrypt with AES-256-GCM, save to SQLite         │
│  7. Update in-memory token, reschedule refresh                          │
│  8. On restart: Load encrypted token from SQLite if still valid         │
└─────────────────────────────────────────────────────────────────────────┘
```

## Configuration

All configuration is via environment variables. **No defaults are used - all values must be explicitly set.**

### Required Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `TELEPHONE_TOKEN` | JWT token from Plugboard (optional if DB has valid token) | `eyJhbGci...` |
| `SECRET_KEY_BASE` | Secret for token encryption (64 char hex) | `6a5c5a634bc0...` |
| `PLUGBOARD_URL` | WebSocket URL to Plugboard | `ws://localhost:4000/telephone/websocket` |
| `BACKEND_HOST` | Backend service hostname | `localhost` |
| `BACKEND_PORT` | Backend service port | `8080` |
| `BACKEND_SCHEME` | Backend URL scheme | `http` or `https` |
| `CONNECT_TIMEOUT` | WebSocket connection timeout | `10s` |
| `REQUEST_TIMEOUT` | HTTP request timeout | `30s` |
| `HEARTBEAT_INTERVAL` | WebSocket heartbeat interval | `30s` |
| `CONNECTION_MONITOR_INTERVAL` | Connection health check interval | `5s` |
| `INITIAL_BACKOFF` | Initial reconnection backoff | `1s` |
| `MAX_BACKOFF` | Maximum reconnection backoff | `30s` |
| `MAX_RETRIES` | Max reconnection retries (-1 = infinite) | `100` |
| `TOKEN_DB_PATH` | Path to SQLite token database | `./telephone.db` |
| `MAX_RESPONSE_SIZE` | Maximum response size in bytes | `104857600` |
| `CHUNK_SIZE` | Chunk size for large responses | `1048576` |
| `DB_TIMEOUT` | Database operation timeout | `10s` |

## Token Storage Schema

Telephone uses SQLite for encrypted token persistence:

**tokens table**

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER | Primary key (auto-increment) |
| `encrypted_token` | TEXT | AES-256-GCM encrypted JWT token (base64) |
| `expires_at` | DATETIME | Token expiration timestamp |
| `updated_at` | DATETIME | Last update timestamp |

**Security:**
- Tokens encrypted using AES-256-GCM authenticated encryption
- Encryption key derived from `SECRET_KEY_BASE` using PBKDF2 (100,000 iterations)
- Database contains only encrypted data - useless without `SECRET_KEY_BASE`

## Phoenix Channels Protocol

Telephone implements the Phoenix Channels wire protocol:

**Message Format:** `[join_ref, ref, topic, event, payload]`

**Key Events:**
- `phx_join` - Join the telephone channel with JWT token
- `phx_reply` - Server response to join/other requests
- `heartbeat` / `heartbeat_ack` - Keep connection alive
- `proxy_req` - Incoming HTTP request from Plugboard
- `proxy_res` - Response sent back to Plugboard
- `refresh_token` - Request new JWT token from Plugboard

## Testing Guidelines

- Run `go test -race ./...` to detect race conditions
- Use table-driven tests for comprehensive coverage
- Mock the `ChannelsClient` interface for unit testing proxy logic
- Integration tests are in `*_integration_test.go` files
- Benchmarks are in `*_benchmark_test.go` files

## Concurrency Model

Telephone uses several goroutines:
1. **Main goroutine** - Manages lifecycle and coordinates shutdown
2. **Read loop** - Reads messages from WebSocket (`channels/client.go`)
3. **Heartbeat** - Sends periodic heartbeats to keep connection alive
4. **Connection monitor** - Checks connection health and triggers reconnection
5. **Token refresh timer** - Schedules token refresh at half-life

All goroutines respect `context.Context` for cancellation and use `sync.WaitGroup` for graceful shutdown.
