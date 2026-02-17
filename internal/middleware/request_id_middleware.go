package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"
const maxRequestIDLen = 128

type contextKey string

const requestIDContextKey contextKey = "request_id"

// RequestID sets/propagates a request ID in context and response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := NormalizeRequestID(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = uuid.NewString()
		}

		w.Header().Set(requestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// NormalizeRequestID returns a safe request ID or empty string when invalid.
func NormalizeRequestID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > maxRequestIDLen {
		return ""
	}
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return ""
	}
	return trimmed
}

func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, ok := ctx.Value(requestIDContextKey).(string)
	if !ok {
		return ""
	}
	return value
}
