# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies (no CGO needed with pure-Go SQLite)
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary (pure Go, no CGO required)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')" \
    -o telephone \
    ./cmd/telephone

# Runtime stage
FROM alpine:3.23

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 telephone && \
    adduser -D -u 1000 -G telephone telephone

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/telephone /usr/local/bin/telephone

# Set ownership
RUN chown -R telephone:telephone /app

# Switch to non-root user
USER telephone

# Set default environment variables
ENV PLUGBOARD_URL=ws://plugboard:4000/telephone/websocket \
    BACKEND_HOST=localhost \
    BACKEND_PORT=8080

ENTRYPOINT ["/usr/local/bin/telephone"]
