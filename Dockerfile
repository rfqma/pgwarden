# syntax=docker/dockerfile:1.6

# ============================================
# Stage 1: Build pgwarden binary
# ============================================
FROM golang:1.25.6-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/pgwarden ./cmd/pgwarden

RUN /out/pgwarden --version

# ============================================
# Stage 2: Runtime image (alpine)
# ============================================
FROM alpine:3.20

ARG VERSION=dev

RUN apk add --no-cache \
        postgresql16-client \
        gnupg \
        ca-certificates \
    && rm -rf /var/cache/apk/*

RUN mkdir -p /app /tmp \
    && adduser -D -u 1000 pgwarden

COPY --from=builder /out/pgwarden /app/pgwarden
COPY scripts/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh /app/pgwarden

LABEL org.opencontainers.image.title="pgwarden" \
      org.opencontainers.image.description="PostgreSQL backup guardian" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.source="https://github.com/rfqma/pgwarden" \
      org.opencontainers.image.licenses="Proprietary"

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD pgwarden health || exit 1

EXPOSE 8080

USER pgwarden
ENTRYPOINT ["/app/entrypoint.sh"]
