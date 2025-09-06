# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build optimized binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags='-w -s' \
    -o dns-proxy .

# Final stage
FROM gcr.io/distroless/static-debian11:nonroot

# Copy binary
COPY --from=builder /app/dns-proxy /dns-proxy

# Expose port (can be overridden)
EXPOSE 5353/udp

# Default environment variables
ENV LISTEN_ADDR=0.0.0.0
ENV LISTEN_PORT=5353
ENV DOCKER_DNS=127.0.0.11:53
ENV UPSTREAM_DNS=8.8.8.8:53
ENV ENABLE_UPSTREAM=false
ENV TIMEOUT_SECONDS=2
ENV LOG_LEVEL=INFO
ENV ENABLE_METRICS=false
ENV STRIP_SUFFIX=.docker

# Run the application
ENTRYPOINT ["/dns-proxy"]