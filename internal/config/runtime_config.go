package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr       = ":9090"
	defaultBackendBaseURL   = "https://2msvdt2oba.execute-api.us-east-1.amazonaws.com/"
	defaultJWKSURL          = ""
	defaultIssuer           = ""
	defaultAudience         = "mcp"
	defaultRateLimitRPS     = 5
	defaultRateLimitBurst   = 10
	defaultLogLevel         = "info"
	defaultRequestBodyBytes = int64(1 << 20) // 1 MiB
	defaultResponseBytes    = int64(2 << 20) // 2 MiB
)

// Config holds runtime configuration for the MCP server.
type Config struct {
	ListenAddr string
	LogLevel   string

	TLS TLSConfig

	Backend BackendConfig
	Auth    AuthConfig

	RateLimit RateLimitConfig
	Limits    LimitConfig
	Server    ServerConfig
}

type TLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

type BackendConfig struct {
	BaseURL string
	Timeout time.Duration
}

type AuthConfig struct {
	JWKSURL              string
	Issuer               string
	Audience             string
	JWKSRefreshInterval  time.Duration
	JWTClockSkew         time.Duration
	AllowedSigningAlgs   []string
	JWKSHTTPTimeout      time.Duration
	UnknownKIDMinRefresh time.Duration
}

type RateLimitConfig struct {
	RPS   float64
	Burst int
}

type LimitConfig struct {
	MaxRequestBodyBytes  int64
	MaxResponseBodyBytes int64
}

type ServerConfig struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

// Load reads environment configuration and applies safe defaults.
func Load() (Config, error) {
	return load(true)
}

// LoadForLambda loads configuration for an API Gateway/Lambda deployment.
// TLS is terminated by API Gateway, so local certificates are not required.
func LoadForLambda() (Config, error) {
	return load(false)
}

func load(requireTLS bool) (Config, error) {
	cfg := Config{
		ListenAddr: getEnv("MCP_LISTEN_ADDR", defaultListenAddr),
		LogLevel:   strings.ToLower(getEnv("LOG_LEVEL", defaultLogLevel)),
		TLS: TLSConfig{
			CertFile: strings.TrimSpace(os.Getenv("MCP_TLS_CERT_FILE")),
			KeyFile:  strings.TrimSpace(os.Getenv("MCP_TLS_KEY_FILE")),
			CAFile:   strings.TrimSpace(os.Getenv("MCP_TLS_CA_FILE")),
		},
		Backend: BackendConfig{
			BaseURL: strings.TrimRight(getEnv("BACKEND_BASE_URL", defaultBackendBaseURL), "/"),
			Timeout: 5 * time.Second,
		},
		Auth: AuthConfig{
			JWKSURL:              getEnv("AUTH_JWKS_URL", defaultJWKSURL),
			Issuer:               getEnv("AUTH_ISSUER", defaultIssuer),
			Audience:             getEnv("AUTH_AUDIENCE", defaultAudience),
			JWKSRefreshInterval:  5 * time.Minute,
			JWTClockSkew:         30 * time.Second,
			AllowedSigningAlgs:   []string{"RS256", "ES256"},
			JWKSHTTPTimeout:      5 * time.Second,
			UnknownKIDMinRefresh: 30 * time.Second,
		},
		RateLimit: RateLimitConfig{
			RPS:   defaultRateLimitRPS,
			Burst: defaultRateLimitBurst,
		},
		Limits: LimitConfig{
			MaxRequestBodyBytes:  defaultRequestBodyBytes,
			MaxResponseBodyBytes: defaultResponseBytes,
		},
		Server: ServerConfig{
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
			ShutdownTimeout:   15 * time.Second,
		},
	}

	rps, err := parseFloatEnv("RATE_LIMIT_RPS", defaultRateLimitRPS)
	if err != nil {
		return Config{}, err
	}
	if rps <= 0 {
		return Config{}, errors.New("RATE_LIMIT_RPS must be greater than 0")
	}
	cfg.RateLimit.RPS = rps

	burst, err := parseIntEnv("RATE_LIMIT_BURST", defaultRateLimitBurst)
	if err != nil {
		return Config{}, err
	}
	if burst <= 0 {
		return Config{}, errors.New("RATE_LIMIT_BURST must be greater than 0")
	}
	cfg.RateLimit.Burst = burst

	if requireTLS {
		if cfg.TLS.CertFile == "" {
			return Config{}, errors.New("MCP_TLS_CERT_FILE is required")
		}
		if cfg.TLS.KeyFile == "" {
			return Config{}, errors.New("MCP_TLS_KEY_FILE is required")
		}
		if cfg.TLS.CAFile == "" {
			return Config{}, errors.New("MCP_TLS_CA_FILE is required")
		}
	}

	if cfg.LogLevel != "debug" && cfg.LogLevel != "info" && cfg.LogLevel != "warn" && cfg.LogLevel != "error" {
		return Config{}, fmt.Errorf("unsupported LOG_LEVEL: %q", cfg.LogLevel)
	}
	if err := validateEndpointURL("BACKEND_BASE_URL", cfg.Backend.BaseURL); err != nil {
		return Config{}, err
	}
	if cfg.Auth.JWKSURL != "" {
		if err := validateEndpointURL("AUTH_JWKS_URL", cfg.Auth.JWKSURL); err != nil {
			return Config{}, err
		}
	}
	if cfg.Auth.Issuer == "" && cfg.Auth.JWKSURL != "" {
		return Config{}, errors.New("AUTH_ISSUER is required when AUTH_JWKS_URL is configured")
	}
	if cfg.Auth.Issuer != "" && cfg.Auth.JWKSURL == "" {
		return Config{}, errors.New("AUTH_JWKS_URL is required when AUTH_ISSUER is configured")
	}

	return cfg, nil
}

// EndpointForLog returns a minimally sensitive URL representation for logs.
func EndpointForLog(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return rawURL
	}
	return parsed.Scheme + "://" + parsed.Host
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func parseFloatEnv(key string, fallback float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}

func validateEndpointURL(envName, rawValue string) error {
	value := strings.TrimSpace(rawValue)
	if value == "" {
		return fmt.Errorf("%s is required", envName)
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", envName, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must include scheme and host", envName)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include embedded credentials", envName)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must not include query params or fragments", envName)
	}

	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return fmt.Errorf("%s must use https for non-loopback hosts", envName)
	default:
		return fmt.Errorf("%s must use http or https", envName)
	}
}

func isLoopbackHost(host string) bool {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return false
	}
	if strings.EqualFold(trimmed, "localhost") {
		return true
	}
	ip := net.ParseIP(trimmed)
	return ip != nil && ip.IsLoopback()
}

func parseIntEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return parsed, nil
}
