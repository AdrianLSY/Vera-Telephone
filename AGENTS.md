This is a WebSocket-based reverse proxy sidecar written in Go.

**Always refer to the `CLAUDE.md` file for a high-level system overview before starting work on this project.**

**After completing any task, review and update `README.md`, `AGENTS.md`, and `CLAUDE.md` as necessary to keep documentation current.**

## Project guidelines

- Use `make precommit` alias when you are done with all changes and fix any pending issues
- Use the standard library `net/http` for HTTP client operations where possible
- Use `github.com/gorilla/websocket` for WebSocket connections (already included)
- Use `github.com/golang-jwt/jwt/v5` for JWT operations (already included)

### Configuration guidelines

- **All application configuration MUST be set via environment variables** - never use hardcoded default values
- Configuration values should be read in `pkg/config/config.go` using `os.Getenv` and return an error if required values are missing
- **Never** provide default values for required configuration - all configuration must come from the environment
- All required environment variables must be documented in `.env.example` and `README.md`
- Duration values should be parsed using `time.ParseDuration` for flexibility (e.g., `10s`, `5m`, `1h`)

### Go guidelines

- Go variables are **not** immutable - they can be reassigned, but be mindful of scope
- **Always** use `context.Context` as the first parameter for functions that perform I/O or may block
- **Never** store contexts in structs - pass them as function parameters
- **Always** handle errors explicitly - never use `_` to ignore errors unless absolutely necessary and documented
- Use `fmt.Errorf("context: %w", err)` to wrap errors with context
- **Always** close resources (files, connections, response bodies) using `defer` immediately after acquiring them:

      resp, err := http.Get(url)
      if err != nil {
          return err
      }
      defer resp.Body.Close()

- **Never** use `panic` for error handling - `panic` is only for unrecoverable programming errors
- Use named return values sparingly - only when they improve readability
- Prefer `errors.Is()` and `errors.As()` over direct error comparison for wrapped errors

### Interface guidelines

- **Accept interfaces, return structs** - this makes code more flexible and testable
- Keep interfaces small and focused - prefer multiple small interfaces over one large one
- Define interfaces where they are used, not where they are implemented
- Use interface composition to build larger interfaces from smaller ones:

      type Reader interface {
          Read(p []byte) (n int, err error)
      }

      type Writer interface {
          Write(p []byte) (n int, err error)
      }

      type ReadWriter interface {
          Reader
          Writer
      }

### Concurrency guidelines

- **Always** manage goroutine lifecycle - ensure goroutines can be stopped gracefully
- Use `context.Context` for cancellation and timeouts in concurrent code
- **Never** start a goroutine without a clear plan for how it will stop
- Use channels for communication between goroutines, mutexes for protecting shared state
- Prefer `sync.WaitGroup` for waiting on multiple goroutines to complete
- Use `select` with `context.Done()` for cancellable operations:

      select {
      case <-ctx.Done():
          return ctx.Err()
      case result := <-resultCh:
          return result
      }

- **Always** use the race detector during testing: `go test -race ./...`
- Avoid goroutine leaks - ensure channels are closed and goroutines exit

### Error handling guidelines

- **Always** wrap errors with context using `fmt.Errorf`:

      if err != nil {
          return fmt.Errorf("failed to connect to backend: %w", err)
      }

- Create custom error types for errors that need to be programmatically handled:

      type ConnectionError struct {
          Host string
          Err  error
      }

      func (e *ConnectionError) Error() string {
          return fmt.Sprintf("connection to %s failed: %v", e.Host, e.Err)
      }

      func (e *ConnectionError) Unwrap() error {
          return e.Err
      }

- Use sentinel errors for known conditions that callers may want to check:

      var ErrTokenExpired = errors.New("token expired")

- Handle errors at the appropriate level - don't just pass them up blindly
- Log errors at the point where you have the most context

### Testing guidelines

- **Always** use table-driven tests for functions with multiple test cases:

      func TestParseToken(t *testing.T) {
          tests := []struct {
              name    string
              token   string
              want    *Claims
              wantErr bool
          }{
              {"valid token", "eyJ...", &Claims{...}, false},
              {"expired token", "eyJ...", nil, true},
              {"malformed token", "invalid", nil, true},
          }

          for _, tt := range tests {
              t.Run(tt.name, func(t *testing.T) {
                  got, err := ParseToken(tt.token)
                  if (err != nil) != tt.wantErr {
                      t.Errorf("ParseToken() error = %v, wantErr %v", err, tt.wantErr)
                      return
                  }
                  if !reflect.DeepEqual(got, tt.want) {
                      t.Errorf("ParseToken() = %v, want %v", got, tt.want)
                  }
              })
          }
      }

- Use subtests (`t.Run`) for organizing related test cases
- **Always** run tests with the race detector in CI: `go test -race ./...`
- Use `*_test.go` files in the same package for unit tests
- Use `_test` package suffix for black-box testing when needed
- Mock external dependencies using interfaces
- Use `t.Helper()` in test helper functions for better error reporting
- Write benchmarks for performance-critical code:

      func BenchmarkParseMessage(b *testing.B) {
          msg := []byte(`[null,"1","topic","event",{}]`)
          for i := 0; i < b.N; i++ {
              ParseMessage(msg)
          }
      }

### HTTP client guidelines

- **Always** set timeouts on HTTP clients - never use the default client:

      client := &http.Client{
          Timeout: 30 * time.Second,
      }

- **Always** close response bodies:

      resp, err := client.Do(req)
      if err != nil {
          return err
      }
      defer resp.Body.Close()

- Use `context.Context` for request cancellation:

      req, err := http.NewRequestWithContext(ctx, "GET", url, nil)

- Reuse HTTP clients when possible - they are safe for concurrent use

### WebSocket guidelines

- Use `github.com/gorilla/websocket` for WebSocket connections
- **Always** handle connection lifecycle (connect, ping/pong, close)
- Implement heartbeat/ping-pong to detect dead connections
- Use separate goroutines for reading and writing with proper synchronization
- Handle reconnection with exponential backoff
- **Always** close WebSocket connections gracefully:

      conn.WriteMessage(websocket.CloseMessage, 
          websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

### JSON handling guidelines

- Use `encoding/json` for JSON marshaling/unmarshaling
- Use struct tags for JSON field names:

      type Message struct {
          ID      string `json:"id"`
          Payload any    `json:"payload,omitempty"`
      }

- Use `json.RawMessage` for delayed parsing of JSON fields
- Handle JSON errors explicitly - don't ignore malformed input

### Logging guidelines

- Use structured logging with `log/slog` (Go 1.21+) or `log` package
- Include relevant context in log messages (correlation IDs, request info)
- Use appropriate log levels (Debug, Info, Warn, Error)
- **Never** log sensitive information (tokens, passwords, API keys)

### Build and tooling guidelines

- **Always** run `go fmt ./...` before committing
- **Always** run `golangci-lint run` and fix all issues
- Use `go mod tidy` to clean up dependencies
- Use build tags for platform-specific code
- Use `make` for common build operations
- Use multi-stage Docker builds for smaller images
- Document all exported functions, types, and constants

### Code organization

- Follow the standard Go project layout:
  - `cmd/` - Main applications
  - `pkg/` - Library code that can be used by external applications
  - `internal/` - Private application code (if needed)
- Keep packages focused and cohesive
- Avoid circular dependencies between packages
- Use meaningful package names (avoid `util`, `common`, `misc`)
