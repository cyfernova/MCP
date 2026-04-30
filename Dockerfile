# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
WORKDIR /src

RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mcp ./cmd/mcp

FROM alpine:3.22

RUN apk add --no-cache ca-certificates caddy openssl \
    && addgroup -S app \
    && adduser -S app -G app

WORKDIR /app

COPY --from=builder /out/mcp /app/mcp

RUN mkdir -p /app/certs \
    && openssl req -x509 -newkey rsa:2048 -keyout /app/certs/ca.key -out /app/certs/ca.crt -days 3650 -nodes -subj "/CN=Billief MCP Internal CA" \
    && openssl req -newkey rsa:2048 -keyout /app/certs/server.key -out /app/certs/server.csr -nodes -subj "/CN=localhost" \
    && openssl x509 -req -in /app/certs/server.csr -CA /app/certs/ca.crt -CAkey /app/certs/ca.key -CAcreateserial -out /app/certs/server.crt -days 3650 \
    && rm -f /app/certs/server.csr /app/certs/ca.key /app/certs/ca.srl \
    && chown -R app:app /app

ENV MCP_TLS_CERT_FILE=/app/certs/server.crt \
    MCP_TLS_KEY_FILE=/app/certs/server.key \
    MCP_TLS_CA_FILE=/app/certs/ca.crt \
    MCP_LISTEN_ADDR=127.0.0.1:9090 \
    LOG_LEVEL=info

EXPOSE 8080

USER app

CMD set -eu; \
    PORT="${PORT:-8080}"; \
    /app/mcp & MCP_PID="$!"; \
    printf ':%s {\n  reverse_proxy https://127.0.0.1:9090 {\n    transport http {\n      tls_insecure_skip_verify\n    }\n  }\n}\n' "$PORT" > /tmp/Caddyfile; \
    caddy run --config /tmp/Caddyfile --adapter caddyfile & CADDY_PID="$!"; \
    trap 'kill "$MCP_PID" "$CADDY_PID" 2>/dev/null || true' TERM INT; \
    while kill -0 "$MCP_PID" 2>/dev/null && kill -0 "$CADDY_PID" 2>/dev/null; do sleep 2; done; \
    kill "$MCP_PID" "$CADDY_PID" 2>/dev/null || true; \
    wait
