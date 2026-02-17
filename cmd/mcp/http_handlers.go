package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cyfernova/mcp/internal/middleware"
	"github.com/cyfernova/mcp/internal/mtls"
)

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

func (a *app) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func (a *app) handleToolsList(w http.ResponseWriter, _ *http.Request) {
	toolsResp := listToolsResponse{Tools: make([]listedTool, 0, len(a.toolReg.List()))}
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
