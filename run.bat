@echo off
REM Run MCP Server on Windows

echo Starting MCP Server...
echo.

REM Check if .env file exists
if not exist .env (
    echo ERROR: .env file not found!
    echo Please copy .env.example to .env and configure it.
    exit /b 1
)

REM Check if certificates exist
if not exist certs\server.crt (
    echo ERROR: TLS certificates not found!
    echo Please run: powershell -ExecutionPolicy Bypass -File generate-certs.ps1
    exit /b 1
)

REM Check if binary exists
if not exist bin\mcp.exe (
    echo ERROR: MCP server binary not found!
    echo Please run: go build -o bin/mcp.exe ./cmd/mcp
    exit /b 1
)

REM Load environment variables from .env
for /f "tokens=*" %%a in ('findstr /v "^#" .env ^| findstr /v "^$"') do set %%a

REM Run the server
bin\mcp.exe
