.PHONY: help build run clean test certs swagger

help:
	@echo "MCP Server Setup"
	@echo ""
	@echo "Available targets:"
	@echo "  make build   - Build the MCP server binary"
	@echo "  make run     - Run the MCP server (requires .env file)"
	@echo "  make test    - Run tests"
	@echo "  make clean   - Clean build artifacts"
	@echo "  make certs   - Generate self-signed TLS certificates for mTLS"
	@echo "  make swagger - Generate swagger.json from tool catalog"
	@echo ""

build:
	go build -o bin/mcp.exe ./cmd/mcp

run: build
	powershell -Command "Get-Content .env | Where-Object { $$_ -notmatch '^#' -and $$_ -match '=' } | ForEach-Object { $$parts = $$_ -split '=', 2; [Environment]::SetEnvironmentVariable($$parts[0].Trim(), $$parts[1].Trim(), 'Process') }; .\bin\mcp.exe"

test:
	go test -v ./...

clean:
	rm -rf bin/

# Generate self-signed TLS certificates for development mTLS
certs:
	@mkdir -p certs
	@echo "Generating CA certificate..."
	openssl req -x509 -newkey rsa:4096 -keyout certs/ca.key -out certs/ca.crt -days 365 -nodes -subj "/CN=MCP Dev CA"
	@echo ""
	@echo "Generating server certificate..."
	openssl req -newkey rsa:4096 -keyout certs/server.key -out certs/server.csr -nodes -subj "/CN=localhost"
	openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/server.crt -days 365
	@echo ""
	@echo "Generating client certificate..."
	openssl req -newkey rsa:4096 -keyout certs/client.key -out certs/client.csr -nodes -subj "/CN=MCP Client"
	openssl x509 -req -in certs/client.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/client.crt -days 365
	@echo ""
	@echo "Cleaning up temporary files..."
	rm -f certs/server.csr certs/client.csr certs/ca.srl
	@echo ""
	@echo "Certificates generated successfully!"
	@echo "  - CA: certs/ca.crt"
	@echo "  - Server: certs/server.crt, certs/server.key"
	@echo "  - Client: certs/client.crt, certs/client.key"

# Generate OpenAPI/Swagger spec from tool catalog and embed in binary
swagger: bin/gen-swagger.exe swagger.json
	cp swagger.json cmd/swagger-assets/swagger.json

swagger.json: bin/gen-swagger.exe
	./bin/gen-swagger.exe swagger.json
