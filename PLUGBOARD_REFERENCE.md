# Plugboard Implementation Reference

**Target Version**: Plugboard Phase 6 (Horde-based distributed registry)  
**Last Updated**: 2025-11-05  
**Purpose**: Reference documentation for Telephone developers on Plugboard's specific implementation

---

## Overview

This document describes the specific implementation details of Plugboard that Telephone must integrate with. It serves as a quick reference for developers working on Telephone without needing to read through Plugboard's Elixir source code.

**Key Files in Plugboard:**
- `lib/plugboard_web/channels/telephone_socket.ex` - WebSocket authentication
- `lib/plugboard_web/channels/telephone_channel.ex` - Channel message handling
- `lib/plugboard_web/controllers/proxy_controller.ex` - HTTP request routing
- `lib/plugboard/telephone_registry.ex` - Telephone process registry
- `lib/plugboard/telephone_tokens.ex` - JWT token management

---

## Authentication & Connection Flow

### 1. WebSocket Connection

**Endpoint**: `wss://{plugboard_host}/socket/websocket`

**Required Query Parameters**:
```
?token={jwt_token}&vsn=2.0.0
```

**Implementation**: `PlugboardWeb.TelephoneSocket.connect/3`

```elixir
def connect(%{"token" => jwt_token}, socket, _connect_info) do
  case TelephoneTokens.validate_and_mark_used(jwt_token) do
    {:ok, %{token: token, path: path, user_id: user_id}} ->
      socket = socket
        |> assign(:token_id, token.id)
        |> assign(:path_id, path.id)
        |> assign(:path, path)
        |> assign(:user_id, user_id)
      {:ok, socket}
    {:error, reason} ->
      :error
  end
end
```

**Socket Assigns After Connect**:
- `token_id` - UUID of the token record
- `path_id` - UUID of the path/mount point
- `path` - Full path struct with metadata
- `user_id` - UUID of the user who created the token

**Failure Cases**:
- Missing `token` parameter → `:error` (WebSocket close)
- Invalid JWT signature → `:error`
- Expired token → `:error`
- Revoked token → `:error`
- Path deleted or not a mount point → `:error`

### 2. Channel Join

**Topic Format**: `"telephone:{path_id}"`

The `path_id` must match `socket.assigns.path_id` or join will fail.

**Implementation**: `PlugboardWeb.TelephoneChannel.join/3`

```elixir
def join("telephone:" <> path_id, _payload, socket) do
  if socket.assigns.path_id == path_id do
    :ok = TelephoneRegistry.register(path_id, self())
    
    socket = socket
      |> assign(:waiting_callers, %{})
      |> assign(:last_heartbeat, System.monotonic_time(:millisecond))
    
    Process.send_after(self(), :check_heartbeat, 60_000)
    
    {:ok, %{
      status: "ok",
      path: socket.assigns.path.full_path,
      expires_in: 3600  # configurable
    }, socket}
  else
    {:error, %{reason: "path_id_mismatch"}}
  end
end
```

**Success Response Payload**:
```json
{
  "status": "ok",
  "path": "/api",
  "expires_in": 3600
}
```

**What Happens on Join**:
1. Telephone process is registered in `TelephoneRegistry` (Horde)
2. `waiting_callers` map initialized (for request correlation)
3. `last_heartbeat` timestamp set
4. Heartbeat check scheduled for 60 seconds
5. Telemetry event emitted: `[:plugboard, :telephone, :connected]`

---

## Heartbeat Mechanism

### Client → Server: Heartbeat

**Event**: `"heartbeat"`

**Payload**:
```json
{
  "ts": 1234567890  // Unix timestamp (optional)
}
```

**Implementation**: `PlugboardWeb.TelephoneChannel.handle_in("heartbeat", ...)`

```elixir
def handle_in("heartbeat", %{"ts" => ts}, socket) do
  socket = assign(socket, :last_heartbeat, System.monotonic_time(:millisecond))
  push(socket, "heartbeat_ack", %{ts: ts})
  {:noreply, socket}
end
```

### Server → Client: Heartbeat Acknowledgment

**Event**: `"heartbeat_ack"`

**Payload**:
```json
{
  "ts": 1234567890  // Echoes back the timestamp you sent
}
```

### Timeout Detection

**Implementation**: Plugboard checks heartbeat every 60 seconds

```elixir
def handle_info(:check_heartbeat, socket) do
  last_heartbeat = Map.get(socket.assigns, :last_heartbeat, 0)
  now = System.monotonic_time(:millisecond)
  timeout_ms = 60_000
  
  if now - last_heartbeat > timeout_ms do
    {:stop, :heartbeat_timeout, socket}
  else
    Process.send_after(self(), :check_heartbeat, timeout_ms)
    {:noreply, socket}
  end
end
```

**Timeout Behavior**:
- Plugboard checks every 60 seconds
- If `now - last_heartbeat > 60,000ms` → disconnect
- On timeout: `:heartbeat_timeout` reason, channel terminates

**Telephone Requirements**:
- Send heartbeat at least every 60 seconds
- Recommended: Every 15-30 seconds for safety margin
- Include timestamp for latency monitoring

---

## Token Refresh

### Client → Server: Refresh Token

**Event**: `"refresh_token"`

**Payload**: `{}` (empty object)

**Implementation**: `PlugboardWeb.TelephoneChannel.handle_in("refresh_token", ...)`

```elixir
def handle_in("refresh_token", _payload, socket) do
  case TelephoneTokens.refresh_token(socket.assigns.token_id) do
    {:ok, new_jwt, expires_in} ->
      push(socket, "refresh_token_ack", %{token: new_jwt, expires_in: expires_in})
      {:noreply, socket}
    {:error, reason} ->
      {:reply, {:error, %{reason: inspect(reason)}}, socket}
  end
end
```

### Server → Client: Refresh Token Acknowledgment

**Event**: `"refresh_token_ack"`

**Success Payload**:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 3600
}
```

**Error Response** (via `phx_reply`):
```json
{
  "status": "error",
  "response": {
    "reason": "token_revoked"
  }
}
```

**What Happens on Refresh**:
1. New JWT generated with same claims but new `iat`/`exp`
2. Token hash updated in database
3. `expires_at` extended by configured duration (default 3600s)
4. Old JWT becomes invalid
5. Telemetry: `[:plugboard, :telephone_token, :refreshed]`

**Telephone Requirements**:
- Store new JWT for reconnection attempts
- Refresh before expiry (recommend 10 minutes before)
- Handle refresh errors (may need to reconnect)

---

## Proxy Request Flow

### Server → Client: Proxy Request

**Event**: `"proxy_req"`

**Payload**:
```json
{
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "method": "GET",
  "path": "/users/123",
  "headers": {
    "host": "example.com",
    "user-agent": "curl/7.68.0",
    "accept": "application/json"
  },
  "body": "",
  "query_string": "foo=bar&baz=qux"
}
```

**Implementation**: `PlugboardWeb.TelephoneChannel.handle_info({:proxy_request, ...})`

```elixir
def handle_info({:proxy_request, from_pid, request_id, request_payload}, socket) do
  push(socket, "proxy_req", request_payload)
  
  waiting_callers = Map.get(socket.assigns, :waiting_callers, %{})
  socket = assign(socket, :waiting_callers, Map.put(waiting_callers, request_id, from_pid))
  
  {:noreply, socket}
end
```

**Field Details**:

| Field | Type | Description | Example |
|-------|------|-------------|---------|
| `request_id` | UUID string | Correlation ID (CRITICAL) | `"550e8400-..."` |
| `method` | String | HTTP method | `"GET"`, `"POST"`, etc. |
| `path` | String | Path with mount stripped | `"/users/123"` |
| `headers` | Object | HTTP headers (lowercase) | `{"host": "example.com"}` |
| `body` | String | Request body | `""` or `"{...}"` |
| `query_string` | String | Raw query without `?` | `"foo=bar"` |

**How Plugboard Generates This**:

From `PlugboardWeb.ProxyController.forward_request_to_telephone/4`:

1. Client sends: `GET /call/api/users/123?foo=bar`
2. `MountStore.match("/api/users/123")` finds mount `/api`
3. Forwarded path: `/users/123` (mount stripped)
4. Generate UUID for `request_id`
5. Read request body
6. Send `{:proxy_request, self(), request_id, payload}` to TelephoneChannel
7. Wait for `{:proxy_res, request_id, response}` with timeout

**Timeout Configuration**:
- Per-path timeout in `paths` table: `request_timeout_ms`
- Default: 60,000ms (60 seconds)
- Valid range: 1-300,000ms
- If Telephone doesn't respond in time → client gets 504 Gateway Timeout

### Client → Server: Proxy Response

**Event**: `"proxy_res"`

**Standard Payload**:
```json
{
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": 200,
  "headers": {
    "content-type": "application/json",
    "content-length": "45"
  },
  "body": "{\"id\":123,\"name\":\"Alice\"}",
  "chunked": false
}
```

**Chunked Payload**:
```json
{
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": 200,
  "headers": {
    "content-type": "text/plain",
    "transfer-encoding": "chunked"
  },
  "body": "",
  "chunked": true,
  "chunks": ["chunk1", "chunk2", "chunk3"]
}
```

**Implementation**: `PlugboardWeb.TelephoneChannel.handle_in("proxy_res", ...)`

```elixir
def handle_in("proxy_res", %{"request_id" => request_id} = payload, socket) do
  send(self(), {:send_proxy_response, request_id, payload})
  {:noreply, socket}
end

def handle_info({:send_proxy_response, request_id, response}, socket) do
  waiting_callers = Map.get(socket.assigns, :waiting_callers, %{})
  
  case Map.get(waiting_callers, request_id) do
    nil ->
      # Late response or unknown request_id - ignored
      {:noreply, socket}
    caller_pid ->
      send(caller_pid, {:proxy_res, request_id, response})
      updated_callers = Map.delete(waiting_callers, request_id)
      {:noreply, assign(socket, :waiting_callers, updated_callers)}
  end
end
```

**What Happens**:
1. Telephone sends `proxy_res` with `request_id`
2. Channel looks up caller PID in `waiting_callers` map
3. If found: Send `{:proxy_res, request_id, response}` to caller
4. Remove from `waiting_callers` map
5. If not found: Log warning, ignore (late or duplicate response)

**Critical: Request ID**
- **MUST** include the same `request_id` from the `proxy_req`
- Without matching `request_id`, response cannot be correlated
- ProxyController is waiting for this specific `request_id`
- Multiple requests can be in-flight simultaneously

---

## Request Correlation Architecture

### How Concurrent Requests Work

Plugboard supports **multiple concurrent requests over a single WebSocket**:

```
ProxyController (Request 1) ──┐
ProxyController (Request 2) ──┼──> TelephoneChannel ──> Telephone
ProxyController (Request 3) ──┘                              │
         ▲                                                   │
         │                                                   │
         └───────── Responses correlated via request_id ────┘
```

**Data Structures**:

In `TelephoneChannel`:
```elixir
socket.assigns.waiting_callers = %{
  "req-uuid-1" => #PID<0.123.0>,
  "req-uuid-2" => #PID<0.124.0>,
  "req-uuid-3" => #PID<0.125.0>
}
```

Each ProxyController process:
```elixir
# Waiting in receive block for specific request_id
receive do
  {:proxy_res, ^request_id, response} -> {:ok, response}
  {:proxy_error, ^request_id, :telephone_disconnected} -> {:error, :telephone_disconnected}
after
  timeout -> {:error, :timeout}
end
```

**Flow**:
1. Client A → Plugboard: `GET /call/api/users/1`
2. Client B → Plugboard: `GET /call/api/users/2`
3. Client C → Plugboard: `GET /call/api/users/3`
4. ProxyController generates UUIDs: `req-A`, `req-B`, `req-C`
5. All three sent to TelephoneChannel → stored in `waiting_callers`
6. TelephoneChannel pushes all three `proxy_req` to Telephone
7. Telephone processes concurrently (goroutines)
8. Responses come back in any order (e.g., B, C, A)
9. TelephoneChannel routes each response to correct ProxyController
10. Clients get responses

**Telephone Requirements**:
- Must handle concurrent `proxy_req` messages
- Must preserve `request_id` in responses
- Recommended: Use goroutines + sync.Map for tracking
- No global queue blocking - each request is independent

---

## Disconnect & Cleanup

### Normal Disconnect

When Telephone closes WebSocket gracefully, Plugboard's `terminate/2` is called.

**Implementation**: `PlugboardWeb.TelephoneChannel.terminate/2`

```elixir
def terminate(reason, socket) do
  TelephoneRegistry.unregister(socket.assigns.path_id, self())
  
  waiting_callers = Map.get(socket.assigns, :waiting_callers, %{})
  
  if map_size(waiting_callers) > 0 do
    Enum.each(waiting_callers, fn {request_id, caller_pid} ->
      send(caller_pid, {:proxy_error, request_id, :telephone_disconnected})
    end)
  end
  
  :telemetry.execute(
    [:plugboard, :telephone, :disconnected],
    %{count: 1, pending_requests: map_size(waiting_callers)},
    %{path_id: socket.assigns.path_id, reason: reason}
  )
  
  :ok
end
```

**What Happens**:
1. Telephone unregistered from `TelephoneRegistry`
2. All pending requests get `{:proxy_error, request_id, :telephone_disconnected}`
3. ProxyController returns 502 Bad Gateway to clients
4. Telemetry event emitted
5. Channel process terminates

**In-Flight Requests**:
- All waiting callers are notified immediately
- ProxyController returns: `502 Bad Gateway - Telephone disconnected during request`
- Clients receive error response, not a timeout

### Crash or Network Failure

Same as normal disconnect, but `reason` will be different (e.g., `:heartbeat_timeout`, `:shutdown`, etc.)

---

## Registry & Load Balancing

### Telephone Registration

**Implementation**: `Plugboard.TelephoneRegistry.register/2` → `Plugboard.DistributedRegistry`

Internally uses Horde.Registry (CRDT-based distributed registry).

**Registration**:
```elixir
# When telephone joins channel
TelephoneRegistry.register(path_id, telephone_pid)
```

**Key**: `path_id` (UUID string)  
**Value**: `telephone_pid` (process PID)  
**Registry Name**: Via Horde, cluster-wide

### Load Balancing

**Implementation**: `Plugboard.TelephoneRegistry.get_telephone/1`

```elixir
def get_telephone(path_id) do
  DistributedRegistry.get_telephone(path_id)
end
```

**Strategy**: Round-robin across all registered telephones for a path

If multiple Telephone instances register for the same `path_id`:
- Requests are distributed round-robin
- Each call to `get_telephone/1` returns the next PID in rotation
- Built into Horde.Registry

**What This Means**:
- Multiple Telephone instances can serve the same path
- Automatic load balancing
- If one Telephone dies, others continue serving
- No sticky sessions - each request may go to different Telephone

---

## Error Responses to Clients

When things go wrong, ProxyController returns these HTTP status codes:

| Scenario | HTTP Status | Response Body |
|----------|-------------|---------------|
| No mount point found | 404 Not Found | `{"reason": "No mount point found for path"}` |
| No telephone registered | 503 Service Unavailable | `{"reason": "No telephone available for this path"}` |
| Telephone process dead | 502 Bad Gateway | `{"reason": "Bad Gateway - Telephone disconnected"}` |
| Telephone timeout | 504 Gateway Timeout | `{"reason": "Gateway Timeout - The telephone did not respond in time"}` |
| Telephone disconnected mid-request | 502 Bad Gateway | `{"reason": "Bad Gateway - Telephone disconnected during request"}` |
| Telephone error | 502 Bad Gateway | `{"reason": "Bad Gateway - Telephone error"}` |
| Path config error | 500 Internal Server Error | `{"reason": "Path configuration not found"}` |

**Implementation**: `PlugboardWeb.HTTPError.send_error/3`

All errors are JSON responses with consistent format:
```json
{
  "error": true,
  "reason": "Human readable message",
  "details": {
    "path": "/api/users",
    "timeout_ms": 60000
  }
}
```

---

## Telemetry Events

Plugboard emits telemetry events that can be useful for debugging:

| Event | Measurements | Metadata | When |
|-------|--------------|----------|------|
| `[:plugboard, :telephone, :connected]` | `%{count: 1}` | `%{path_id, path}` | Telephone joins channel |
| `[:plugboard, :telephone, :disconnected]` | `%{count: 1, pending_requests: N}` | `%{path_id, path, reason}` | Telephone disconnects |
| `[:plugboard, :telephone, :proxy_request]` | `%{duration: ns}` | `%{path_id, method, status}` | Request completed |
| `[:plugboard, :telephone, :proxy_timeout]` | `%{count: 1}` | `%{path_id, timeout_ms}` | Request timeout |
| `[:plugboard, :telephone, :slow_response]` | `%{duration: ns, timeout: ms}` | `%{path_id, method}` | Response >80% of timeout |
| `[:plugboard, :telephone, :unavailable]` | `%{count: 1}` | `%{path_id}` | No telephone available |
| `[:plugboard, :telephone, :chunk_error]` | `%{count: 1}` | `%{reason}` | Chunked response error |

These can be observed via telemetry handlers for monitoring/debugging.

---

## Configuration (Plugboard Side)

These environment variables affect Telephone behavior:

| Variable | Default | Description |
|----------|---------|-------------|
| `TELEPHONE_TOKEN_EXPIRY` | 3600 | Token expiry in seconds |
| `TELEPHONE_TOKEN_REFRESH_INTERVAL` | 1800 | Unused (client-side decision) |
| `SECRET_KEY_BASE` | (required) | JWT signing secret |
| `MAX_REQUEST_BODY_SIZE` | 10485760 | Max body size (10MB) |

**Token Expiry**:
- Configurable via `config :plugboard, :telephone, token_expiry: 3600`
- Returned in join response: `expires_in` field
- Used when refreshing tokens

**Request Timeout**:
- Stored per-path in database: `paths.request_timeout_ms`
- Default: 60,000ms
- Valid range: 1 - 300,000ms
- Invalid values → fallback to 60,000ms with warning

---

## Chunked Response Handling

**Implementation**: `PlugboardWeb.ProxyController.send_chunked_response/3`

```elixir
defp send_chunked_response(conn, status, chunks) do
  conn = conn
    |> put_status(status)
    |> send_chunked(status)
  
  Enum.reduce_while(chunks, {:ok, conn}, fn chunk, {:ok, acc_conn} ->
    case chunk(acc_conn, chunk) do
      {:ok, new_conn} -> {:cont, {:ok, new_conn}}
      {:error, reason} ->
        Logger.error("Error sending chunk: #{inspect(reason)}")
        {:halt, {:error, reason, acc_conn}}
    end
  end)
end
```

**What This Means**:
- Plugboard uses Phoenix's `send_chunked/2` + `chunk/2`
- Chunks are sent sequentially (not streamed incrementally)
- All chunks must be in the `chunks` array
- Chunk errors are logged but client may see partial response

**Telephone's Responsibility**:
- Collect all chunks from backend
- Include in single `proxy_res` message
- Set `chunked: true` and populate `chunks: [...]`
- Each chunk is a string

---

## Phoenix Channels Protocol Details

### Message Format

All WebSocket messages are JSON arrays:
```json
[join_ref, msg_ref, topic, event, payload]
```

**Phoenix Protocol Version**: `2.0.0` (required in connection URL)

### Message Types

**Client → Server**:
- `phx_join` - Join a channel
- `phx_leave` - Leave a channel (graceful)
- `heartbeat` - Custom heartbeat (not Phoenix heartbeat)
- `refresh_token` - Request token refresh
- `proxy_res` - Proxy response

**Server → Client**:
- `phx_reply` - Reply to client messages (join, etc.)
- `phx_close` - Channel closed
- `heartbeat_ack` - Heartbeat acknowledgment
- `refresh_token_ack` - Token refresh response
- `proxy_req` - Proxy request

### Connection Lifecycle

1. **WebSocket Connect**: `wss://host/socket/websocket?token=...&vsn=2.0.0`
2. **Socket Connect Handler**: Validates JWT, assigns socket state
3. **Client sends phx_join**: `["1", "1", "telephone:path-id", "phx_join", {}]`
4. **Server replies**: `[null, "1", "telephone:path-id", "phx_reply", {...}]`
5. **Normal operation**: Exchange messages
6. **Disconnect**: Client closes or server sends `phx_close`

---

## Testing Against Plugboard

### Prerequisites

1. **Running Plugboard instance**
2. **Database with mount point** (`paths` table)
3. **Valid JWT token** (from `telephone_tokens` table)
4. **Path ID** (UUID of the mount point)

### Manual Testing Steps

1. **Connect**: Open WebSocket to `wss://plugboard/socket/websocket?token={jwt}&vsn=2.0.0`
2. **Join**: Send `["1", "1", "telephone:{path_id}", "phx_join", {}]`
3. **Expect**: `[null, "1", "telephone:{path_id}", "phx_reply", {"status":"ok",...}]`
4. **Heartbeat**: Send `[null, "2", "telephone:{path_id}", "heartbeat", {"ts":123}]`
5. **Expect**: `[null, null, "telephone:{path_id}", "heartbeat_ack", {"ts":123}]`
6. **Trigger Request**: `curl http://plugboard/call/api/test`
7. **Expect**: `[null, null, "telephone:{path_id}", "proxy_req", {...}]`
8. **Respond**: Send `[null, "3", "telephone:{path_id}", "proxy_res", {...}]`
9. **Verify**: `curl` receives response

### Integration Test Checklist

- [ ] Valid JWT → successful connection
- [ ] Invalid JWT → connection rejected
- [ ] Expired JWT → connection rejected
- [ ] Channel join with correct path_id → success
- [ ] Channel join with wrong path_id → error
- [ ] Heartbeat sent → heartbeat_ack received
- [ ] No heartbeat for 60s → connection closed
- [ ] Token refresh → new JWT received
- [ ] Proxy request → proxy_req message received
- [ ] Proxy response → client receives HTTP response
- [ ] Multiple concurrent requests → all responses received
- [ ] Disconnect mid-request → client gets 502
- [ ] No response within timeout → client gets 504

---

## Version Compatibility

**This document describes**: Plugboard Phase 6 (Horde-based distributed registry)

**Key Identifiers**:
- Uses `Plugboard.DistributedRegistry` (Horde)
- JWT authentication in WebSocket connection
- Request correlation via `request_id` (UUID)
- Phoenix Channels protocol version 2.0.0

**Breaking Changes to Watch For**:
- Changes to JWT claims structure
- Changes to message payload schemas
- Changes to timeout behavior
- Addition of new required fields

**Future Enhancements** (not yet implemented):
- Binary DATA frames for large bodies
- FLOW control for backpressure
- Request cancellation (CANCEL event)
- Trailers support
- Multi-domain routing

---

## Quick Reference: Message Examples

### Connection Sequence
```json
// 1. Join channel
["1", "1", "telephone:path-uuid", "phx_join", {}]

// 2. Server replies
[null, "1", "telephone:path-uuid", "phx_reply", {
  "status": "ok",
  "response": {"status": "ok", "path": "/api", "expires_in": 3600}
}]

// 3. Start heartbeats
[null, "2", "telephone:path-uuid", "heartbeat", {"ts": 1234567890}]
[null, null, "telephone:path-uuid", "heartbeat_ack", {"ts": 1234567890}]
```

### Request/Response
```json
// Server → Client: proxy_req
[null, null, "telephone:path-uuid", "proxy_req", {
  "request_id": "req-uuid",
  "method": "GET",
  "path": "/users/123",
  "headers": {"host": "example.com"},
  "body": "",
  "query_string": "active=true"
}]

// Client → Server: proxy_res
[null, "3", "telephone:path-uuid", "proxy_res", {
  "request_id": "req-uuid",
  "status": 200,
  "headers": {"content-type": "application/json"},
  "body": "{\"id\":123}",
  "chunked": false
}]
```

### Token Refresh
```json
// Client → Server
[null, "4", "telephone:path-uuid", "refresh_token", {}]

// Server → Client
[null, null, "telephone:path-uuid", "refresh_token_ack", {
  "token": "eyJhbGc...",
  "expires_in": 3600
}]
```

---

## Summary for Developers

**Key Takeaways**:

1. **Authentication**: JWT in WebSocket URL, validated on connect
2. **Protocol**: Phoenix Channels 2.0.0 (JSON arrays)
3. **Heartbeat**: Send every 15-30s, server timeout at 60s
4. **Request Correlation**: Always preserve `request_id` from proxy_req
5. **Concurrency**: Support multiple in-flight requests simultaneously
6. **Timeouts**: Default 60s, configurable per-path
7. **Errors**: Return proper HTTP status in proxy_res
8. **Disconnect**: All pending requests get 502 error
9. **Token Refresh**: Refresh 10 min before expiry
10. **Load Balancing**: Multiple Telephone instances supported (round-robin)

**Most Common Mistakes**:
- ❌ Forgetting to include `request_id` in proxy_res
- ❌ Not sending heartbeats → connection timeout
- ❌ Blocking on requests (must handle concurrently)
- ❌ Not refreshing token → auth expiry
- ❌ Wrong Phoenix message format (must be 5-element array)

---

This reference should be updated whenever Plugboard's implementation changes in ways that affect Telephone integration.
