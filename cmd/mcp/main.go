package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cyfernova/mcp/internal/auth"
	"github.com/cyfernova/mcp/internal/backend"
	"github.com/cyfernova/mcp/internal/config"
	"github.com/cyfernova/mcp/internal/middleware"
	"github.com/cyfernova/mcp/internal/mtls"
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

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	backendClient, err := backend.NewClient(cfg.Backend.BaseURL, cfg.Backend.Timeout, cfg.Limits.MaxResponseBodyBytes)
	if err != nil {
		log.Error("failed to create backend client", "error", err)
		os.Exit(1)
	}

	verifier, err := auth.NewVerifier(ctx, auth.VerifyConfig{
		Issuer:               cfg.Auth.Issuer,
		Audience:             cfg.Auth.Audience,
		AllowedSigningAlgs:   cfg.Auth.AllowedSigningAlgs,
		ClockSkew:            cfg.Auth.JWTClockSkew,
		JWKSURL:              cfg.Auth.JWKSURL,
		JWKSRefreshInterval:  cfg.Auth.JWKSRefreshInterval,
		JWKSHTTPTimeout:      cfg.Auth.JWKSHTTPTimeout,
		UnknownKIDMinRefresh: cfg.Auth.UnknownKIDMinRefresh,
	}, log.With("component", "auth"))
	if err != nil {
		log.Error("failed to initialize JWT verifier", "error", err)
		os.Exit(1)
	}

	a := &app{
		cfg:         cfg,
		log:         log,
		verifier:    verifier,
		rateLimiter: middleware.NewRateLimiter(cfg.RateLimit.RPS, cfg.RateLimit.Burst),
		backend:     backendClient,
	}
	a.toolReg, err = tools.NewRegistry()
	if err != nil {
		log.Error("failed to initialize tool registry", "error", err)
		os.Exit(1)
	}
	a.mcpServer = a.newMCPServer()

	streamableHandler := mcpgo.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcpgo.Server {
			return a.mcpServer
		},
		&mcpgo.StreamableHTTPOptions{
			Stateless:      true,
			JSONResponse:   true,
			Logger:         log.With("component", "mcp_streamable_http"),
			SessionTimeout: 10 * time.Minute,
		},
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.Handle("POST /mcp/tools/list", a.requireMTLS(http.HandlerFunc(a.handleToolsList)))
	mux.Handle("POST /mcp/tools/call", a.requireMTLS(http.HandlerFunc(a.handleToolsCall)))
	mux.Handle("/mcp", a.requireMTLS(streamableHandler))
	mux.Handle("/mcp/", a.requireMTLS(streamableHandler))

	handler := middleware.RequestID(middleware.Logging(log)(mux))

	tlsCfg, err := mtls.LoadServerTLSConfig(cfg.TLS.CertFile, cfg.TLS.KeyFile, cfg.TLS.CAFile)
	if err != nil {
		log.Error("failed to configure mTLS", "error", err)
		os.Exit(1)
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	go func() {
		log.Info("starting MCP server",
			"listen_addr", cfg.ListenAddr,
			"backend_base_url", cfg.Backend.BaseURL,
			"jwks_url", cfg.Auth.JWKSURL,
		)
		if err := httpServer.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server exited with error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("server stopped")
}

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
						requestID = strings.TrimSpace(req.Extra.Header.Get("X-Request-ID"))
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

func (a *app) requireMTLS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !mtls.HasVerifiedClientCert(r) {
			writeError(w, r, &apiError{
				HTTPStatus: http.StatusUnauthorized,
				Code:       "mtls_required",
				Message:    "client certificate is required",
			})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, a.cfg.Limits.MaxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (a *app) handleToolsList(w http.ResponseWriter, r *http.Request) {
	if !mtls.HasVerifiedClientCert(r) {
		writeError(w, r, &apiError{HTTPStatus: http.StatusUnauthorized, Code: "mtls_required", Message: "client certificate is required"})
		return
	}

	toolsResp := listToolsResponse{Tools: make([]listedTool, 0)}
	for _, def := range a.toolReg.List() {
		toolsResp.Tools = append(toolsResp.Tools, listedTool{
			Name:        def.Name,
			Description: def.Description,
			Schema:      def.InputSchema,
		})
	}

	writeJSON(w, http.StatusOK, toolsResp)
}

func (a *app) handleToolsCall(w http.ResponseWriter, r *http.Request) {
	if !mtls.HasVerifiedClientCert(r) {
		writeError(w, r, &apiError{HTTPStatus: http.StatusUnauthorized, Code: "mtls_required", Message: "client certificate is required"})
		return
	}

	var req callToolRequest
	if err := decodeJSONStrict(w, r, a.cfg.Limits.MaxRequestBodyBytes, &req); err != nil {
		writeError(w, r, &apiError{HTTPStatus: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()})
		return
	}

	req.Tool = strings.TrimSpace(req.Tool)
	if req.Tool == "" {
		writeError(w, r, &apiError{HTTPStatus: http.StatusBadRequest, Code: "invalid_request", Message: "tool is required"})
		return
	}

	requestID := middleware.GetRequestID(r.Context())
	if requestID == "" {
		requestID = uuid.NewString()
	}

	result, status, route, _, execErr := a.executeToolCall(r.Context(), req.Tool, req.Args, r.Header.Get("Authorization"), requestID, "compat")
	if execErr != nil {
		writeError(w, r, execErr)
		return
	}

	writeJSON(w, http.StatusOK, callToolResponse{
		Result:      result,
		Status:      status,
		RequestID:   requestID,
		BackendPath: route,
	})
}

func (a *app) executeToolCall(ctx context.Context, toolName string, rawArgs json.RawMessage, authHeader string, requestID string, transport string) (json.RawMessage, int, string, string, *apiError) {
	started := time.Now()
	status := http.StatusInternalServerError
	backendRoute := ""
	routeFamily := ""
	userID := ""

	defer func() {
		a.log.Info("tool_audit",
			"request_id", requestID,
			"user_id", userID,
			"tool", toolName,
			"backend_route", backendRoute,
			"route_family", routeFamily,
			"status", status,
			"latency_ms", time.Since(started).Milliseconds(),
			"transport", transport,
		)
	}()

	prepared, err := a.toolReg.Prepare(toolName, rawArgs)
	if err != nil {
		if errors.Is(err, tools.ErrToolNotFound) {
			status = http.StatusNotFound
			return nil, status, backendRoute, routeFamily, &apiError{HTTPStatus: status, Code: "tool_not_found", Message: "tool is not allowlisted"}
		}
		status = http.StatusBadRequest
		return nil, status, backendRoute, routeFamily, &apiError{HTTPStatus: status, Code: "invalid_args", Message: err.Error()}
	}
	backendRoute = prepared.BackendRoute
	routeFamily = prepared.RouteFamily

	bearerToken := ""
	authHeader = strings.TrimSpace(authHeader)
	if authHeader != "" {
		bearerToken, err = parseBearerToken(authHeader)
		if err != nil {
			status = http.StatusUnauthorized
			return nil, status, backendRoute, routeFamily, &apiError{
				HTTPStatus: status,
				Code:       "unauthorized",
				Message:    "missing or invalid bearer token",
			}
		}
	}

	if routeRequiresAuth(prepared.Method, prepared.Path) && bearerToken == "" {
		status = http.StatusUnauthorized
		return nil, status, backendRoute, routeFamily, &apiError{
			HTTPStatus: status,
			Code:       "unauthorized",
			Message:    "missing or invalid bearer token",
		}
	}

	if bearerToken != "" {
		principal, verifyErr := a.verifier.Verify(ctx, bearerToken)
		if verifyErr != nil {
			status = http.StatusUnauthorized
			return nil, status, backendRoute, routeFamily, &apiError{
				HTTPStatus: status,
				Code:       "unauthorized",
				Message:    "delegated token verification failed",
			}
		}
		userID = principal.UserID
	}

	rateLimitKey := userID
	if rateLimitKey == "" {
		rateLimitKey = "anonymous"
	}
	if !a.rateLimiter.Allow(rateLimitKey) {
		status = http.StatusTooManyRequests
		return nil, status, backendRoute, routeFamily, &apiError{
			HTTPStatus: status,
			Code:       "rate_limited",
			Message:    "rate limit exceeded",
		}
	}

	resp, err := a.backend.Do(ctx, backend.Request{
		Method:      prepared.Method,
		Path:        prepared.Path,
		Query:       prepared.Query,
		Body:        prepared.JSONBody,
		Headers:     prepared.Headers,
		BearerToken: bearerToken,
		RequestID:   requestID,
	})
	if err != nil {
		if errors.Is(err, backend.ErrResponseTooLarge) {
			status = http.StatusBadGateway
			return nil, status, backendRoute, routeFamily, &apiError{HTTPStatus: status, Code: "backend_response_too_large", Message: "backend response exceeded limit"}
		}
		status = http.StatusBadGateway
		return nil, status, backendRoute, routeFamily, &apiError{HTTPStatus: status, Code: "backend_unavailable", Message: "backend request failed"}
	}
	status = resp.StatusCode

	if resp.StatusCode >= http.StatusBadRequest {
		details := map[string]any{"backend_status": resp.StatusCode}
		if json.Valid(resp.Body) {
			details["backend_error"] = json.RawMessage(resp.Body)
		}
		return nil, resp.StatusCode, backendRoute, routeFamily, &apiError{
			HTTPStatus: resp.StatusCode,
			Code:       "backend_error",
			Message:    "backend returned an error",
			Details:    details,
		}
	}

	if len(resp.Body) == 0 {
		return json.RawMessage(`{}`), resp.StatusCode, backendRoute, routeFamily, nil
	}
	if !json.Valid(resp.Body) {
		status = http.StatusBadGateway
		return nil, status, backendRoute, routeFamily, &apiError{
			HTTPStatus: status,
			Code:       "backend_invalid_response",
			Message:    "backend returned non-JSON response",
		}
	}

	return json.RawMessage(resp.Body), resp.StatusCode, backendRoute, routeFamily, nil
}

func parseBearerToken(header string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", fmt.Errorf("invalid authorization header")
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}
	return token, nil
}

func routeRequiresAuth(method, path string) bool {
	switch method + " " + path {
	case
		"POST /api/v1/auth/register",
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/logout",
		"POST /api/v1/auth/refresh",
		"POST /api/v1/auth/forgot-password",
		"POST /api/v1/auth/reset-password",
		"POST /api/v1/auth/verify-email",
		"POST /api/v1/auth/resend-verification":
		return false
	default:
		return true
	}
}

func decodeJSONStrict(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	defer r.Body.Close()

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("request body must contain a single JSON object")
	}

	return nil
}

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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, `{"error":{"code":"internal_error","message":"failed to encode JSON response"}}`, http.StatusInternalServerError)
	}
}

func newLogger(level string) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
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
