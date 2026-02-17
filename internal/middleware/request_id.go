package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"

type contextKey string

const requestIDContextKey contextKey = "request_id"

// RequestID sets/propagates a request ID in context and response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = uuid.NewString()
		}

		w.Header().Set(requestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
