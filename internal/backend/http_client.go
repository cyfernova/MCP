package backend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const requestIDHeader = "X-Request-ID"

var ErrResponseTooLarge = errors.New("backend response exceeds allowed size")

var blockedForwardHeaders = map[string]struct{}{
	"Authorization":       {},
	"Connection":          {},
	"Content-Length":      {},
	"Forwarded":           {},
	"Host":                {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
	"X-Forwarded-For":     {},
	"X-Forwarded-Host":    {},
	"X-Forwarded-Proto":   {},
	"X-Real-Ip":           {},
	"X-Request-Id":        {},
}

type Request struct {
	Method      string
	Path        string
	Query       url.Values
	Body        []byte
	Headers     map[string]string
	BearerToken string
	RequestID   string
}

// Response wraps backend response details.
type Response struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

// Client performs bounded, timeout-aware calls to the backend.
type Client struct {
	baseURL              *url.URL
	httpClient           *http.Client
	timeout              time.Duration
	maxResponseBodyBytes int64
}

func NewClient(baseURL string, timeout time.Duration, maxResponseBodyBytes int64) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse backend base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid BACKEND_BASE_URL: %q", baseURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("BACKEND_BASE_URL must use http or https, got %q", parsed.Scheme)
	}
	if parsed.User != nil {
		return nil, errors.New("BACKEND_BASE_URL must not include credentials")
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &Client{
		baseURL: parsed,
		httpClient: &http.Client{
			Transport: transport,
		},
		timeout:              timeout,
		maxResponseBodyBytes: maxResponseBodyBytes,
	}, nil
}

func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if !strings.HasPrefix(req.Path, "/") {
		return nil, fmt.Errorf("backend path must be absolute: %q", req.Path)
	}
	if req.Method == "" {
		return nil, fmt.Errorf("backend method is required")
	}

	requestURL := c.baseURL.ResolveReference(&url.URL{
		Path:     req.Path,
		RawQuery: req.Query.Encode(),
	})

	var bodyReader io.Reader
	if len(req.Body) > 0 {
		bodyReader = bytes.NewReader(req.Body)
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(requestCtx, req.Method, requestURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build backend request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")
	if strings.TrimSpace(req.BearerToken) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.BearerToken)
	}
	if trimmedRequestID := strings.TrimSpace(req.RequestID); trimmedRequestID != "" {
		httpReq.Header.Set(requestIDHeader, trimmedRequestID)
	}
	for key, value := range req.Headers {
		rawKey := strings.TrimSpace(key)
		if rawKey == "" || !isValidHeaderName(rawKey) {
			continue
		}

		canonical := http.CanonicalHeaderKey(rawKey)
		if canonical == "" {
			continue
		}
		if _, blocked := blockedForwardHeaders[canonical]; blocked {
			continue
		}
		if hasControlChars(value) {
			continue
		}
		httpReq.Header.Set(canonical, strings.TrimSpace(value))
	}
	if len(req.Body) > 0 && strings.TrimSpace(httpReq.Header.Get("Content-Type")) == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send backend request: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, c.maxResponseBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read backend response: %w", err)
	}
	if int64(len(body)) > c.maxResponseBodyBytes {
		return nil, ErrResponseTooLarge
	}

	return &Response{
		StatusCode: httpResp.StatusCode,
		Body:       body,
		Headers:    httpResp.Header.Clone(),
	}, nil
}

func hasControlChars(value string) bool {
	for _, r := range value {
		if r == '\n' || r == '\r' {
			return true
		}
	}
	return false
}

func isValidHeaderName(name string) bool {
	for _, r := range name {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}
