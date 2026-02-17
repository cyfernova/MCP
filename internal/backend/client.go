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
	httpReq.Header.Set(requestIDHeader, req.RequestID)
	for key, value := range req.Headers {
		canonical := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if canonical == "" {
			continue
		}
		switch canonical {
		case "Authorization", "Content-Length", "Host", "X-Request-Id":
			continue
		}
		httpReq.Header.Set(canonical, value)
	}
	if len(req.Body) > 0 {
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
