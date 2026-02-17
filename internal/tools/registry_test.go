package tools

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestPrepareRejectsUnknownTopLevelFields(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	spec, ok := findToolSpec("GET", "/api/v1/auth/me")
	if !ok {
		t.Fatal("missing GET /api/v1/auth/me tool")
	}

	_, err = reg.Prepare(spec.Name, json.RawMessage(`{"unexpected":true}`))
	if err == nil {
		t.Fatal("expected validation error for unknown top-level field")
	}
	if !errors.Is(err, ErrInvalidToolArg) {
		t.Fatalf("expected ErrInvalidToolArg, got %v", err)
	}
}

func TestPrepareRequiresPathParams(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	spec, ok := findToolSpec("POST", "/api/v1/invoices/:id/send")
	if !ok {
		t.Fatal("missing POST /api/v1/invoices/:id/send tool")
	}

	_, err = reg.Prepare(spec.Name, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected validation error for missing path params")
	}
	if !errors.Is(err, ErrInvalidToolArg) {
		t.Fatalf("expected ErrInvalidToolArg, got %v", err)
	}
}

func TestPrepareBuildsPathQueryAndHeaders(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	spec, ok := findToolSpec("GET", "/api/v1/a2a/v0.3/tasks")
	if !ok {
		t.Fatal("missing GET /api/v1/a2a/v0.3/tasks tool")
	}

	prepared, err := reg.Prepare(spec.Name, json.RawMessage(`{"query":{"page":2,"pageSize":30,"sessionId":"abc"}}`))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Method != http.MethodGet {
		t.Fatalf("expected GET method, got %s", prepared.Method)
	}
	if got := prepared.Query.Get("page"); got != "2" {
		t.Fatalf("expected page=2, got %q", got)
	}
	if got := prepared.Query.Get("pageSize"); got != "30" {
		t.Fatalf("expected pageSize=30, got %q", got)
	}
	if got := prepared.Query.Get("sessionId"); got != "abc" {
		t.Fatalf("expected sessionId=abc, got %q", got)
	}
}

func TestPrepareAllowsRouteSpecificHeaders(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	spec, ok := findToolSpec("POST", "/api/v1/business-profiles/:id/logo")
	if !ok {
		t.Fatal("missing POST /api/v1/business-profiles/:id/logo tool")
	}

	prepared, err := reg.Prepare(spec.Name, json.RawMessage(`{"path":{"id":"b_123"},"headers":{"Content-Type":"image/png"}}`))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepared.Path != "/api/v1/business-profiles/b_123/logo" {
		t.Fatalf("unexpected resolved path: %s", prepared.Path)
	}
	if got := prepared.Headers["Content-Type"]; got != "image/png" {
		t.Fatalf("expected Content-Type header, got %q", got)
	}
}

func TestPrepareRejectsHeaderWithControlChars(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}

	spec, ok := findToolSpec("POST", "/api/v1/business-profiles/:id/logo")
	if !ok {
		t.Fatal("missing POST /api/v1/business-profiles/:id/logo tool")
	}

	_, err = reg.Prepare(spec.Name, json.RawMessage(`{"path":{"id":"b_123"},"headers":{"Content-Type":"image/png\r\nx-extra: injected"}}`))
	if err == nil {
		t.Fatal("expected validation error for header control characters")
	}
	if !errors.Is(err, ErrInvalidToolArg) {
		t.Fatalf("expected ErrInvalidToolArg, got %v", err)
	}
}
