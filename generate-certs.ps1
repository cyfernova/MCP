# Generate Self-Signed TLS Certificates for MCP Server (mTLS)

Write-Host "Generating TLS certificates for MCP Server..." -ForegroundColor Green

# Create certs directory if it doesn't exist
if (-not (Test-Path "certs")) {
    New-Item -ItemType Directory -Path "certs" | Out-Null
}

# Generate CA certificate
Write-Host "`n1. Generating CA certificate..." -ForegroundColor Yellow
& openssl req -x509 -newkey rsa:2048 -nodes -keyout certs/ca.key -out certs/ca.crt -days 365 -subj "//CN=MCPDevCA"

# Generate server certificate
Write-Host "`n2. Generating server certificate..." -ForegroundColor Yellow
& openssl req -newkey rsa:2048 -nodes -keyout certs/server.key -out certs/server.csr -subj "//CN=localhost"
& openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/server.crt -days 365
Remove-Item certs/server.csr -Force -ErrorAction SilentlyContinue
Remove-Item certs/ca.srl -Force -ErrorAction SilentlyContinue

# Generate client certificate
Write-Host "`n3. Generating client certificate..." -ForegroundColor Yellow
& openssl req -newkey rsa:2048 -nodes -keyout certs/client.key -out certs/client.csr -subj "//CN=MCPClient"
& openssl x509 -req -in certs/client.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/client.crt -days 365
Remove-Item certs/client.csr -Force -ErrorAction SilentlyContinue
Remove-Item certs/ca.srl -Force -ErrorAction SilentlyContinue

Write-Host "`nCertificates generated successfully!" -ForegroundColor Green
Write-Host "`nGenerated files:" -ForegroundColor Cyan
Write-Host "  - certs/ca.crt       (CA Certificate)"
Write-Host "  - certs/ca.key       (CA Private Key)"
Write-Host "  - certs/server.crt    (Server Certificate)"
Write-Host "  - certs/server.key    (Server Private Key)"
Write-Host "  - certs/client.crt    (Client Certificate)"
Write-Host "  - certs/client.key    (Client Private Key)"

# List files
Write-Host "`nContents of certs directory:" -ForegroundColor Cyan
Get-ChildItem certs/ | Format-Table Name, Length
