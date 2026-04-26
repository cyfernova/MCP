# MCP Server - Setup Complete

## ✅ Setup Status

Your MCP Server is now ready to run! Here's what has been set up:

- ✅ Go dependencies downloaded
- ✅ TLS certificates generated for mTLS (self-signed)
- ✅ Server binary built (`bin/mcp.exe`)
- ✅ Configuration file created (`.env`)
- ✅ Helper scripts created

## 📁 Project Structure

```
MCP/
├── bin/
│   └── mcp.exe          # Compiled server binary (13MB)
├── certs/
│   ├── ca.crt           # Certificate Authority
│   ├── ca.key           # CA Private Key
│   ├── server.crt       # Server Certificate
│   ├── server.key       # Server Private Key
│   ├── client.crt       # Client Certificate (for testing)
│   └── client.key       # Client Private Key (for testing)
├── .env                 # Environment configuration
├── .env.example         # Example configuration
├── generate-certs.ps1   # Certificate generation script
├── run.bat              # Windows batch script to run server
├── test-health.bat      # Test server health check
├── Makefile             # Make targets for Linux/Mac
└── SETUP.md             # Detailed setup guide
```

## 🚀 Quick Start (Windows)

### Option 1: Using Batch Script

```cmd
run.bat
```

### Option 2: Manual

```cmd
# 1. Set environment variables from .env
set /p MCP_TLS_CERT_FILE=<.env
# ... (or use a tool like direnv)

# 2. Run the server
bin\mcp.exe
```

### Option 3: Using Go

```cmd
go run ./cmd/mcp
```

## 🔧 Configuration

Edit `.env` to customize your setup:

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_LISTEN_ADDR` | Server listen address | `:9090` |
| `MCP_TLS_CERT_FILE` | Server TLS certificate | `./certs/server.crt` |
| `MCP_TLS_KEY_FILE` | Server TLS private key | `./certs/server.key` |
| `MCP_TLS_CA_FILE` | CA certificate | `./certs/ca.crt` |
| `BACKEND_BASE_URL` | Backend API URL | `https://2msvdt2oba.execute-api.us-east-1.amazonaws.com/` |
| `AUTH_JWKS_URL` | JWKS endpoint | `https://2msvdt2oba.execute-api.us-east-1.amazonaws.com/.well-known/jwks.json` |
| `AUTH_ISSUER` | JWT issuer | `your-backend` |
| `AUTH_AUDIENCE` | JWT audience | `mcp` |
| `RATE_LIMIT_RPS` | Rate limit (requests/sec) | `5` |
| `RATE_LIMIT_BURST` | Rate limit burst | `10` |
| `LOG_LEVEL` | Logging level | `info` |

## 🧪 Testing

### Health Check (requires client certificate)

```cmd
test-health.bat
```

Or manually:

```cmd
curl -k --cert certs\client.crt --key certs\client.key https://localhost:9090/health
```

Expected response:
```json
{
  "status": "ok",
  "timestamp": "2026-02-21T..."
}
```

### List Available Tools

```cmd
curl -k --cert certs\client.crt --key certs\client.key ^
  -H "Authorization: Bearer YOUR_JWT_TOKEN" ^
  https://localhost:9090/mcp/tools/list
```

### Call a Tool

```cmd
curl -k --cert certs\client.crt --key certs\client.key ^
  -H "Authorization: Bearer YOUR_JWT_TOKEN" ^
  -H "Content-Type: application/json" ^
  -X POST https://localhost:9090/mcp/tools/call ^
  -d "{\"tool\": \"get_agents\", \"args\": {}}"
```

## 📚 Available Tools

The MCP Server provides access to **100+ tools** organized into families:

### Tool Families

- **agents** - AI agent management
- **invoices** - Invoice CRUD operations
- **products** - Product management
- **customers** - Customer management
- **vendors** - Vendor management
- **payments** - Payment processing
- **marketplace** - Marketplace operations
- **business_profiles** - Business profile management
- **teams** - Team member management
- **webhooks** - Webhook configuration
- **workflows** - Workflow management
- **a2a** - Agent-to-agent communication
- **bargaining** - Negotiation/bargaining
- **discovery** - Agent discovery
- **ledger** - Financial ledger
- **subscriptions** - Subscription management
- **auth** - Authentication operations
- **ws** - WebSocket operations

### Example Tools

- `get_agents` - List all agents
- `get_invoices` - List invoices
- `create_product` - Create a new product
- `get_customers` - List customers
- `post_agents_config` - Create agent configuration
- `post_a2a_message` - Send agent-to-agent message
- `get_bargaining_negotiations` - List negotiations

## 🔐 Security Features

### 1. Mutual TLS (mTLS)
All endpoints require a valid client certificate. The generated certificates:
- Server: `certs/server.crt`, `certs/server.key`
- Client: `certs/client.crt`, `certs/client.key`
- CA: `certs/ca.crt`

### 2. JWT Authentication
Most tools require a valid JWT token from your backend.

### 3. Rate Limiting
Configurable per-user rate limiting (default: 5 RPS, burst 10).

### 4. Input Validation
All tool arguments are validated against JSON schemas.

### 5. Header Filtering
Dangerous headers are blocked from forwarding to the backend.

## 🔍 Server Logs

The server logs to stdout with structured JSON:

```
{"level":"INFO","time":"...","msg":"starting MCP server","listen_addr":":9090","backend_base_url":"https://2msvdt2oba.execute-api.us-east-1.amazonaws.com","jwks_url":"https://2msvdt2oba.execute-api.us-east-1.amazonaws.com/.well-known/jwks.json"}
{"level":"INFO","time":"...","component":"http_request","request_id":"...","method":"GET","path":"/health","status":200,"latency_ms":5,"remote_addr":"..."}
```

## 🔄 Development Workflow

### Rebuild the Server

```cmd
go build -o bin/mcp.exe ./cmd/mcp
```

### Regenerate Certificates

```cmd
powershell -ExecutionPolicy Bypass -File generate-certs.ps1
```

### Run Tests

```cmd
go test -v ./...
```

### Regenerate Tool Catalog

If the backend API changes:

```cmd
go run ./cmd/gen-catalog/main.go --mcp-root . --backend-root ../invoice-backend
```

## 🐛 Troubleshooting

### Server won't start

1. Check `.env` file exists and is valid
2. Verify TLS certificates exist in `certs/` directory
3. Ensure port `9090` is not in use
4. Check logs for detailed error messages

### "mtls_required" error

All endpoints require a client certificate. Use:
```cmd
curl -k --cert certs\client.crt --key certs\client.key ...
```

### "unauthorized" error

Most tools require a valid JWT token. Obtain one from your backend's `/api/v1/auth/login` endpoint.

### Backend connection errors

1. Verify `BACKEND_BASE_URL` in `.env` is correct
2. Ensure backend server is running
3. Check firewall settings

### Certificate errors

The generated certificates are self-signed and for development only. For production:
- Use certificates signed by a trusted CA
- Configure proper certificate rotation
- Use stronger key sizes (4096-bit)

## 📖 Additional Resources

- **SETUP.md** - Detailed setup guide
- **cmd/gen-catalog/main.go** - Tool catalog generator
- **internal/tools/** - Tool registry and validation
- **internal/auth/** - JWT verification
- **internal/backend/** - HTTP client for backend

## ⚠️ Important Notes

1. **Development Mode**: Generated certificates are for development only
2. **Backend Required**: The server requires a running backend API
3. **JWT Required**: Most tools need valid JWT authentication
4. **mTLS Only**: All endpoints require mutual TLS
5. **Tool Allowlist**: Only predefined tools can be called

## 🎯 Next Steps

1. Configure `BACKEND_BASE_URL` to point to your actual backend
2. Update `AUTH_JWKS_URL`, `AUTH_ISSUER`, and `AUTH_AUDIENCE` as needed
3. Obtain a valid JWT token from your backend
4. Test the server with your backend API
5. (Optional) Replace development certificates with production ones

---

For detailed documentation, see `SETUP.md`.
