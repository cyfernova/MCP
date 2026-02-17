package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cyfernova/mcp/internal/middleware"
)

func toolErrorResult(errResp *apiError, requestID string) *mcpgo.CallToolResult {
	payload := map[string]any{
		"code":       errResp.Code,
		"message":    errResp.Message,
		"request_id": requestID,
	}
	if len(errResp.Details) > 0 {
		payload["details"] = errResp.Details
	}
	body, _ := json.Marshal(payload)
	return &mcpgo.CallToolResult{
		IsError: true,
		Content: []mcpgo.Content{&mcpgo.TextContent{Text: string(body)}},
	}
}

func toolSuccessResult(payload json.RawMessage) *mcpgo.CallToolResult {
	result := &mcpgo.CallToolResult{
		Content: []mcpgo.Content{&mcpgo.TextContent{Text: string(payload)}},
	}

	var asObject map[string]any
	if err := json.Unmarshal(payload, &asObject); err == nil {
		result.StructuredContent = asObject
	}
	return result
}

func writeError(w http.ResponseWriter, r *http.Request, errResp *apiError) {
	if errResp.HTTPStatus == 0 {
		errResp.HTTPStatus = http.StatusInternalServerError
	}

	payload := map[string]any{
		"error": map[string]any{
			"code":       errResp.Code,
			"message":    errResp.Message,
			"request_id": middleware.GetRequestID(r.Context()),
		},
	}
	if len(errResp.Details) > 0 {
		payload["error"].(map[string]any)["details"] = errResp.Details
	}

	writeJSON(w, errResp.HTTPStatus, payload)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"failed to encode JSON response"}}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

func newLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	return slog.New(handler)
}

func exitWithConfigError(err error) {
	fmt.Fprintf(os.Stderr, "config error: %v\n", err)
	os.Exit(1)
}
