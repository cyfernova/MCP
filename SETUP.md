# Setup Guide for MCP Server

## Prerequisites

- Go 1.25.5 or higher
- OpenSSL (for TLS certificate generation)
- Access to the backend API at `https://2msvdt2oba.execute-api.us-east-1.amazonaws.com/` (or configurable)

## Quick Start

### 1. Install Dependencies

```bash
go mod download
```

### 2. Generate TLS Certificates (for mTLS)

```bash
make certs
```

This will create:
- `certs/ca.crt` - Certificate Authority
- `certs/server.crt` & `certs/server.key` - Server certificate and key
- `certs/client.crt` & `certs/client.key` - Client certificate and key

### 3. Configure Environment

```bash
cp .env.example .env
```

Edit `.env` with your configuration:
- Update `BACKEND_BASE_URL` to point to your backend API
- Update `AUTH_JWKS_URL` to your JWKS endpoint
- Update `AUTH_ISSUER` and `AUTH_AUDIENCE` as needed

### 4. Build the Server

```bash
make build
```

### 5. Run the Server

```bash
make run
```

Or manually:
```bash
./bin/mcp.exe
```

## Testing the Server

### Health Check (requires client certificate)

```bash
curl -k --cert certs/client.crt --key certs/client.key https://localhost:9090/health
```

Expected response:
```json
{
  "status": "ok",
  "timestamp": "2026-02-21T..."
}
```

### List Available Tools (requires mTLS + JWT)

```bash
curl -k --cert certs/client.crt --key certs/client.key \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  https://localhost:9090/mcp/tools/list
```

### Call a Tool (requires mTLS + JWT)

```bash
curl -k --cert certs/client.crt --key certs/client.key \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST https://localhost:9090/mcp/tools/call \
  -d '{
    "tool": "get_agents",
    "args": {}
  }'
```

## MCP Protocol

The server also supports the standard MCP protocol:

```bash
curl -k --cert certs/client.crt --key certs/client.key \
  -H "Content-Type: application/json" \
  -X POST https://localhost:9090/mcp \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2024-11-05",
      "capabilities": {}
    }
  }'
```

## Configuration Reference

### Required Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_TLS_CERT_FILE` | Server TLS certificate | (required) |
| `MCP_TLS_KEY_FILE` | Server TLS private key | (required) |
| `MCP_TLS_CA_FILE` | CA certificate for client verification | (required) |
| `BACKEND_BASE_URL` | Backend API base URL | `https://2msvdt2oba.execute-api.us-east-1.amazonaws.com/` |
| `AUTH_JWKS_URL` | JWKS endpoint for JWT verification | `https://2msvdt2oba.execute-api.us-east-1.amazonaws.com/.well-known/jwks.json` |

### Optional Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_LISTEN_ADDR` | Server listen address | `:9090` |
| `AUTH_ISSUER` | Expected JWT issuer | `your-backend` |
| `AUTH_AUDIENCE` | Expected JWT audience | `mcp` |
| `RATE_LIMIT_RPS` | Rate limit requests per second | `5` |
| `RATE_LIMIT_BURST` | Rate limit burst size | `10` |
| `LOG_LEVEL` | Logging level (debug/info/warn/error) | `info` |

## Security Notes

1. **mTLS**: All API endpoints require mutual TLS authentication
2. **JWT**: Most tools require a valid JWT token (except public auth endpoints)
3. **Rate Limiting**: Configurable rate limiting per user ID
4. **Header Filtering**: Dangerous headers are blocked from forwarding

## Public Routes (No JWT Required)

The following routes don't require JWT authentication:
- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/logout`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/forgot-password`
- `POST /api/v1/auth/reset-password`
- `POST /api/v1/auth/verify-email`
- `POST /api/v1/auth/resend-verification`

## Development

### Running Tests

```bash
make test
```

### Regenerating Tool Catalog

If the backend API changes, regenerate the tool catalog:

```bash
go run ./cmd/gen-catalog/main.go --mcp-root . --backend-root ../invoice-backend
```

## Troubleshooting

### Server fails to start with TLS errors

Make sure:
1. TLS certificate paths in `.env` are correct
2. Certificate files exist and are readable
3. CA file includes the client certificate's signing CA

### "mtls_required" error

All API endpoints require a valid client certificate. Use:
```bash
curl -k --cert certs/client.crt --key certs/client.key ...
```

### "unauthorized" error

Most tools require a valid JWT token. Obtain one from your backend's `/api/v1/auth/login` endpoint.

### Backend connection errors

Ensure:
1. `BACKEND_BASE_URL` is correct and accessible
2. Backend server is running
3. No firewall blocking the connection
