# Architecture and Quality Review

## 1. Codebase Structure & Request Flow
- **Entrypoints**: `cmd/telephone/main.go` loads `.env`, builds config, constructs the proxy client, and manages lifecycle signals; `cmd/token-check/main.go` provides a diagnostics CLI for JWT tokens and persisted entries.
- **Configuration**: `pkg/config` enforces required environment variables for connection, timeouts, reconnection, storage, and response limits; builds backend URL helpers.
- **Transport/Channels Layer**: `pkg/channels` implements a Phoenix Channels WebSocket client (connect/disconnect, ref-tracking, message routing, and per-connection contexts) and message codec helpers.
- **Proxy/Application Layer**: `pkg/proxy.Telephone` orchestrates startup, channel join, heartbeat, token refresh, reconnection, request validation, backend forwarding, and response fan-out.
- **Persistence Layer**: `pkg/storage.TokenStore` manages encrypted token persistence in SQLite with schema initialization, save/load, cleanup, and stats helpers.
- **Auth Utilities**: `pkg/auth` provides JWT parsing (verified and unsafe), claim validation, and time helpers used across proxy and CLI.
- **Request Flow**: `main` → `config.LoadFromEnv` → `proxy.New` (loads/validates token via `auth`, sets up WebSocket client and token store) → `Telephone.Start` (connects `channels.Client`, joins topic, starts heartbeat/refresh/monitor goroutines) → `channels.Client.readLoop` dispatches events → `Telephone.handleProxyRequest` validates payload and calls `forwardToBackend` → backend HTTP call → `sendProxyResponse` sends Phoenix response over WebSocket → token refresh loop persists/updates tokens.

## 2. Gaps & Architectural Issues
- **Heartbeat health tracking ignores acknowledgements**: `handleHeartbeatAck` only logs and never updates `lastHeartbeat`, so `monitorConnection`’s timeout check is driven solely by send timestamps rather than server acks, risking missed liveness failures (`pkg/proxy/telephone.go`).
- **Token verification is always unsafe**: Both initial token load and refresh flows use `auth.ParseJWTUnsafe`, accepting any token structure without signature verification even when a secret is configured, allowing forged tokens to be accepted (`pkg/proxy/telephone.go`).
- **Unbounded request headers and path composition**: `forwardToBackend` copies all headers and concatenates the path/query string without normalization, so hostile upstream payloads can inject large/invalid headers or malformed URLs with no size limits or canonicalization (`pkg/proxy/telephone.go`).
- **Chunked response handling retains full body in memory**: Response streaming appends each chunk to a slice and also tracks total size; large but permitted bodies (up to `MAX_RESPONSE_SIZE`) are fully buffered, defeating chunking’s memory benefits (`pkg/proxy/telephone.go`).
- **Database schema lacks constraints for token uniqueness/rotation**: The `tokens` table has no unique constraint or pruning triggers, so refresh loops can accumulate unlimited rows and rely solely on manual cleanup calls, increasing DB size and startup cost (`pkg/storage/token_store.go`).

## 3. Code Quality Evaluation
- **Tight coupling of concerns**: `Telephone` mixes connection lifecycle, heartbeat, token refresh, reconnection logic, request validation, and HTTP proxying within a single type (~650 lines), reducing cohesion and testability (`pkg/proxy/telephone.go`).
- **Error handling asymmetry**: Many operations log and continue without propagating (e.g., failed proxy response send, token store cleanup), obscuring upstream visibility and retry control (`pkg/proxy/telephone.go`).
- **Inconsistent JWT handling**: `auth.ParseJWT` supports verification but is unused; pervasive use of unsafe parsing shows pattern inconsistency and weakens assurances (`pkg/auth/jwt.go`, `pkg/proxy/telephone.go`).
- **WebSocket client lacks write/read deadlines and backpressure**: `channels.Client` doesn’t set deadlines or buffer limits when sending/receiving, risking hangs under slow or stalled connections (`pkg/channels/client.go`).

## 4. Test Suite Evaluation
- **Coverage**: Unit tests cover message encoding/decoding, channel client behavior, token store persistence, token refresh, reconnection, and proxy request handling via mocks/integration helpers, but gaps remain for heartbeat timeout handling, header/path validation, and error propagation around backend failures.
- **Behavior vs. implementation**: Tests often assert exact logged messages or specific mock call counts, coupling to implementation rather than observable behavior; integration tests rely on mock clients rather than real WebSocket timing.
- **Isolation**: Tests use temp directories and in-memory structures appropriately (e.g., token store tests), but long-running goroutine loops (heartbeat/monitor) lack deterministic controls, which could be flaky under timing variations.

## 5. Risks & Technical Debt
- **Reliability**: Missing heartbeat ack tracking and absent timeouts on WebSocket I/O can allow dead connections to appear healthy, causing stalled proxying and delayed reconnection.
- **Security**: Unsafe JWT parsing permits forged tokens, undermining authentication; unbounded header/path acceptance could enable header injection or path traversal if upstream data is not trusted.
- **Performance**: Whole-response buffering in `forwardToBackend` can consume up to `MAX_RESPONSE_SIZE` bytes of memory per request; lack of DB cleanup constraints may bloat SQLite and slow startup queries.
- **Maintainability**: The monolithic `Telephone` type and shared state locks across many responsibilities raise the risk of hidden races and make future feature additions harder without refactoring.

## 6. Targeted Recommendations
1. **Track heartbeat acknowledgements**: Update `handleHeartbeatAck` to record the ack timestamp and have `monitorConnection` compare against that marker; add unit tests for heartbeat timeout recovery.
2. **Enforce JWT verification**: Use `auth.ParseJWT` with the secret for initial and refreshed tokens; fail fast when verification fails and extend tests to cover signature failures.
3. **Harden request validation**: Add limits for header counts/lengths and normalize/sanitize paths before concatenation; reject overly long query strings to avoid backend abuse.
4. **Stream responses without full buffering**: Switch to streaming chunk delivery (e.g., send chunks as they arrive) or cap in-memory accumulation to a small window; add tests covering large responses and `MAX_RESPONSE_SIZE` breaches.
5. **Add DB constraints/cleanup policy**: Introduce a unique constraint on `updated_at` or a max-row retention policy and trigger cleanup during startup to prevent unbounded growth.
6. **Reduce coupling**: Extract heartbeat/monitoring, token management, and proxy forwarding into smaller components with interfaces to simplify testing and reuse.
7. **WebSocket resilience**: Set read/write deadlines and consider send buffering or rate limiting to avoid goroutine leaks under slow consumers.

## 7. Database Schema & Query Review
- **Schema**: `tokens` table columns—`id` PK, `encrypted_token` (TEXT, required), `expires_at`, `updated_at`, and `created_at` defaulted; indexes on `updated_at DESC` and `expires_at DESC` for retrieval/cleanup (`pkg/storage/token_store.go`).
- **Correctness/constraints**: No uniqueness on `encrypted_token` or `(expires_at, updated_at)`, so duplicates accumulate; `expires_at` accepts any future time, and no check ensures consistency with token contents.
- **Index strategy**: Indexes align with queries that fetch the latest valid token and purge expired ones, but lack coverage for `expires_at > now` filters combined with `LIMIT 1`; consider a composite index on `(expires_at DESC, updated_at DESC)` for the load path.
- **Query alignment**: `LoadToken` performs `ORDER BY updated_at DESC LIMIT 1` with a `WHERE expires_at > now` filter; without composite index, SQLite will still scan the filtered set. `CleanupExpiredTokens` deletes by `expires_at < now` and benefits from the existing `expires_at` index.
- **Scalability**: No cap on token rows means the table can grow indefinitely; single-connection pool mitigates locking but can become a bottleneck if background cleanup and refresh happen concurrently.
- **Recommendations**: Add a composite index on `(expires_at DESC, updated_at DESC)`, a uniqueness constraint on `encrypted_token` (or on the derived token hash), and a background or startup cleanup to prune old rows; consider storing token metadata (e.g., path_id, sub) to validate consistency before use.
