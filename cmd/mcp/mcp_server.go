package main
// 
import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	// 
	"github.com/google/uuid"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cyfernova/mcp/internal/middleware"
)

func (a *app) newMCPServer() *mcpgo.Server {
	server := mcpgo.NewServer(&mcpgo.Implementation{
		Name:    "invoice-backend-mcp",
		Version: "1.0.0",
	}, &mcpgo.ServerOptions{
		Logger: a.log.With("component", "mcp_server"),
	})

	for _, def := range a.toolReg.List() {
		toolDef := def
		server.AddTool(&mcpgo.Tool{
			Name:        toolDef.Name,
			Description: toolDef.Description,
			InputSchema: toolDef.InputSchema,
		}, func(ctx context.Context, req *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			requestID := middleware.GetRequestID(ctx)
			authHeader := ""
			args := json.RawMessage(`{}`)

			if req != nil {
				if req.Extra != nil {
					authHeader = req.Extra.Header.Get("Authorization")
					if requestID == "" {
						requestID = middleware.NormalizeRequestID(req.Extra.Header.Get("X-Request-ID"))
					}
				}
				if req.Params != nil && len(bytes.TrimSpace(req.Params.Arguments)) > 0 {
					args = req.Params.Arguments
				}
			}
			if requestID == "" {
				requestID = uuid.NewString()
			}

			result, status, route, _, execErr := a.executeToolCall(ctx, toolDef.Name, args, authHeader, requestID, "streamable")
			if execErr != nil {
				return toolErrorResult(execErr, requestID), nil
			}

			a.log.Info("mcp_tool_result",
				"request_id", requestID,
				"tool", toolDef.Name,
				"backend_route", route,
				"route_family", toolDef.Family,
				"status", status,
				"transport", "streamable",
			)

			return toolSuccessResult(result), nil
		})
	}

	return server
}

func parseBearerToken(header string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errInvalidAuthorizationHeader
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", errInvalidAuthorizationHeader
	}
	return token, nil
}
