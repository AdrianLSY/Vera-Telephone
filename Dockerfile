# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies (CGO required for sqlite3)
RUN apk add --no-cache git ca-certificates gcc musl-dev sqlite-dev

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with CGO enabled for sqlite3
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')" \
    -o telephone \
    ./cmd/telephone

# Runtime stage
FROM alpine:3.23

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata sqlite-libs

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
