# =============================================================================
# OFF Barcode Lookup Server — Go
# Multi-stage: golang (build) → alpine (runtime)
# Platform: linux/amd64 (Intel Mac / TATOOINE)
# =============================================================================

# --- Stage 1: Build ---
FROM golang:1.24-bookworm AS builder

WORKDIR /src

# Install DuckDB C library (required by go-duckdb CGo bindings)
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Cache Go modules
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

# Copy source
COPY . .

# Tidy and download deps
RUN go mod tidy

# Build static binary
RUN CGO_ENABLED=1 \
    go build -ldflags="-s -w" -o /off-barcode-server ./cmd/server

# --- Stage 2: Runtime ---
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary
COPY --from=builder /off-barcode-server .

# Copy entrypoint
COPY scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Create data directories
RUN mkdir -p /data/images

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=120s --retries=3 \
    CMD curl -f http://localhost:8080/api/v1/stats || exit 1

ENTRYPOINT ["/entrypoint.sh"]
CMD ["/app/off-barcode-server"]
