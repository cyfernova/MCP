package backend

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestDoBlocksSensitiveHeadersAndPreservesContentType(t *testing.T) {
	client, err := NewClient("https://api.example.com", 2*time.Second, 1<<20)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var captured *http.Request
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			captured = req.Clone(req.Context())
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
	}

	_, err = client.Do(context.Background(), Request{
		Method: http.MethodPost,
		Path:   "/api/v1/upload",
		Body:   []byte(`{"foo":"bar"}`),
		Headers: map[string]string{
			"X-Forwarded-For": "1.2.3.4",
			"Connection":      "keep-alive",
			"X-Custom":        "hello",
			"Content-Type":    "image/png",
			"bad header":      "ignored",
		},
		RequestID: "req_test_123",
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured == nil {
		t.Fatal("expected request to be captured")
	}

	if got := captured.Header.Get("X-Forwarded-For"); got != "" {
		t.Fatalf("X-Forwarded-For should be blocked, got %q", got)
	}
	if got := captured.Header.Get("Connection"); got != "" {
		t.Fatalf("Connection should be blocked, got %q", got)
	}
	if got := captured.Header.Get("X-Custom"); got != "hello" {
		t.Fatalf("X-Custom should be forwarded, got %q", got)
	}
	if got := captured.Header.Get("bad header"); got != "" {
		t.Fatalf("invalid header should be dropped, got %q", got)
	}
	if got := captured.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type should be preserved, got %q", got)
	}
	if got := captured.Header.Get("X-Request-ID"); got != "req_test_123" {
		t.Fatalf("X-Request-ID should be forwarded, got %q", got)
	}
}

func TestDoPreservesBackendBaseURLPathPrefix(t *testing.T) {
	client, err := NewClient("https://api.example.com/dev", 2*time.Second, 1<<20)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var captured *http.Request
	client.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			captured = req.Clone(req.Context())
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
	}

	_, err = client.Do(context.Background(), Request{
		Method: http.MethodGet,
		Path:   "/api/v1/invoices",
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if captured == nil {
		t.Fatal("expected request to be captured")
	}
	if got, want := captured.URL.Path, "/dev/api/v1/invoices"; got != want {
		t.Fatalf("backend path = %q, want %q", got, want)
	}
}
