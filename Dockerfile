# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /gale ./cmd/gale

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates docker-cli

WORKDIR /app

# Copy binary from builder
COPY --from=builder /gale /usr/local/bin/gale
COPY config.yaml /app/config.yaml

# Create non-root user
RUN adduser -D -u 1000 gale && \
    chown -R gale:gale /app

USER gale

ENTRYPOINT ["/usr/local/bin/gale"]
CMD ["-config", "/app/config.yaml"]
