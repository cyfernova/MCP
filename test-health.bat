@echo off
REM Test MCP Server Health Check (requires mTLS)

echo Testing MCP Server Health Check...
echo.

REM Check if certificates exist
if not exist certs\client.crt (
    echo ERROR: Client certificates not found!
    echo Please run: powershell -ExecutionPolicy Bypass -File generate-certs.ps1
    exit /b 1
)

echo Request: GET https://localhost:9090/health
echo.

REM Perform health check with client certificate
curl -k --cert certs\client.crt --key certs\client.key https://localhost:9090/health

echo.
