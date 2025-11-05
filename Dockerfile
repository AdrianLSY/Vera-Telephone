# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=$(git describe --tags --always --dirty)" \
    -o telephone \
    ./cmd/telephone

# Runtime stage
FROM alpine:latest

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

# Expose health check port
EXPOSE 9090

# Set default environment variables
ENV PLUGBOARD_URL=ws://plugboard:4000/telephone/websocket \
    BACKEND_HOST=localhost \
    BACKEND_PORT=8080 \
    HEALTH_PORT=9090

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9090/health || exit 1

ENTRYPOINT ["/usr/local/bin/telephone"]
