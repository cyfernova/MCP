package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.statusCode = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(body []byte) (int, error) {
	if sr.statusCode == 0 {
		sr.statusCode = http.StatusOK
	}
	return sr.ResponseWriter.Write(body)
}

// Logging emits structured request logs with latency and request_id.
func Logging(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(recorder, r)

			status := recorder.statusCode
			if status == 0 {
				status = http.StatusOK
			}

			log.Info("http_request",
				"request_id", GetRequestID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"latency_ms", time.Since(started).Milliseconds(),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}
