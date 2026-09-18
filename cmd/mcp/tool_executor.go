package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cyfernova/mcp/internal/backend"
	"github.com/cyfernova/mcp/internal/tools"
)

var errInvalidAuthorizationHeader = errors.New("invalid authorization header")

var publicToolRoutes = map[string]struct{}{
	"POST /api/v1/auth/register":            {},
	"POST /api/v1/auth/login":               {},
	"POST /api/v1/auth/logout":              {},
	"POST /api/v1/auth/refresh":             {},
	"POST /api/v1/auth/forgot-password":     {},
	"POST /api/v1/auth/reset-password":      {},
	"POST /api/v1/auth/verify-email":        {},
	"POST /api/v1/auth/resend-verification": {},
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
		if a.verifier == nil {
			status = http.StatusServiceUnavailable
			return nil, status, backendRoute, routeFamily, &apiError{
				HTTPStatus: status,
				Code:       "jwt_verification_not_configured",
				Message:    "JWT verification is not configured",
			}
		}
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
		if backendRequestID := strings.TrimSpace(resp.Headers.Get("X-Request-ID")); backendRequestID != "" {
			details["backend_request_id"] = backendRequestID
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

func routeRequiresAuth(method, path string) bool {
	_, isPublic := publicToolRoutes[method+" "+path]
	return !isPublic
}
