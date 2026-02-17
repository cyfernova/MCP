package main

import (
	"encoding/json"
	"log/slog"

	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cyfernova/mcp/internal/auth"
	"github.com/cyfernova/mcp/internal/backend"
	"github.com/cyfernova/mcp/internal/config"
	"github.com/cyfernova/mcp/internal/middleware"
	"github.com/cyfernova/mcp/internal/tools"
)

type app struct {
	cfg config.Config
	log *slog.Logger

	verifier    *auth.Verifier
	rateLimiter *middleware.RateLimiter
	toolReg     *tools.Registry
	backend     *backend.Client
	mcpServer   *mcpgo.Server
}

type listToolsResponse struct {
	Tools []listedTool `json:"tools"`
}

type listedTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
}

type callToolRequest struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

type callToolResponse struct {
	Result      json.RawMessage `json:"result"`
	Status      int             `json:"status"`
	RequestID   string          `json:"request_id"`
	BackendPath string          `json:"backend_route"`
}

type apiError struct {
	HTTPStatus int
	Code       string
	Message    string
	Details    map[string]any
}
