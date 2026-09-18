package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cyfernova/mcp/cmd/swagger-assets"
	"github.com/cyfernova/mcp/internal/auth"
	"github.com/cyfernova/mcp/internal/backend"
	"github.com/cyfernova/mcp/internal/config"
	"github.com/cyfernova/mcp/internal/middleware"
	"github.com/cyfernova/mcp/internal/mtls"
	"github.com/cyfernova/mcp/internal/tools"
)

func newApp(ctx context.Context, cfg config.Config, log *slog.Logger) (*app, error) {
	backendClient, err := backend.NewClient(cfg.Backend.BaseURL, cfg.Backend.Timeout, cfg.Limits.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("create backend client: %w", err)
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
		return nil, fmt.Errorf("initialize JWT verifier: %w", err)
	}

	toolReg, err := tools.NewRegistry()
	if err != nil {
		return nil, fmt.Errorf("initialize tool registry: %w", err)
	}

	a := &app{
		cfg:         cfg,
		log:         log,
		verifier:    verifier,
		rateLimiter: middleware.NewRateLimiter(cfg.RateLimit.RPS, cfg.RateLimit.Burst),
		backend:     backendClient,
		toolReg:     toolReg,
	}
	a.mcpServer = a.newMCPServer()
	return a, nil
}

func (a *app) newHTTPServer() (*http.Server, error) {
	handler := a.newHTTPHandler(true)

	tlsCfg, err := mtls.LoadServerTLSConfig(a.cfg.TLS.CertFile, a.cfg.TLS.KeyFile, a.cfg.TLS.CAFile)
	if err != nil {
		return nil, fmt.Errorf("configure mTLS: %w", err)
	}

	return &http.Server{
		Addr:              a.cfg.ListenAddr,
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: a.cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       a.cfg.Server.ReadTimeout,
		WriteTimeout:      a.cfg.Server.WriteTimeout,
		IdleTimeout:       a.cfg.Server.IdleTimeout,
	}, nil
}

// newHTTPHandler builds the protocol handler shared by server and Lambda
// deployments. API Gateway terminates TLS, so Lambda skips the direct-server
// client-certificate check while retaining JWT authorization.
func (a *app) newHTTPHandler(enforceMTLS bool) http.Handler {
	streamableHandler := mcpgo.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcpgo.Server {
			return a.mcpServer
		},
		&mcpgo.StreamableHTTPOptions{
			Stateless:      true,
			JSONResponse:   true,
			Logger:         a.log.With("component", "mcp_streamable_http"),
			SessionTimeout: 10 * time.Minute,
		},
	)

	mux := http.NewServeMux()

	// Swagger UI static files
	swaggerFS := http.FS(swaggerassets.Assets)
	mux.HandleFunc("GET /swagger/", func(w http.ResponseWriter, r *http.Request) {
		// Serve index.html at /swagger/ and /swagger
		if r.URL.Path == "/swagger" || r.URL.Path == "/swagger/" {
			r.URL.Path = "/swagger/index.html"
		}
		http.FileServer(swaggerFS).ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /swagger.json", func(w http.ResponseWriter, r *http.Request) {
		http.FileServer(swaggerFS).ServeHTTP(w, r)
	})

	// Health check (no auth required)
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.Handle("POST /mcp/tools/list", http.HandlerFunc(a.handleToolsList))
	mux.Handle("POST /mcp/tools/call", http.HandlerFunc(a.handleToolsCall))
	mux.Handle("/mcp", streamableHandler)
	mux.Handle("/mcp/", streamableHandler)

	var handler http.Handler = mux
	if enforceMTLS {
		handler = a.requireMTLS(handler)
	}
	return middleware.RequestID(middleware.Logging(a.log)(handler))
}
