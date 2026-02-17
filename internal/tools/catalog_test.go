package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGeneratedRouterHashMatchesSource(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve caller file")
	}

	mcpRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	routerPath := filepath.Clean(filepath.Join(mcpRoot, GeneratedBackendRouterPath))

	src, err := os.ReadFile(routerPath)
	if err != nil {
		t.Fatalf("read router file %s: %v", routerPath, err)
	}
	hash := sha256.Sum256(src)
	got := hex.EncodeToString(hash[:])
	if got != GeneratedRouterFileSHA256 {
		t.Fatalf("router drift detected: generated hash=%s current hash=%s (run: go generate ./internal/tools)", GeneratedRouterFileSHA256, got)
	}
}

func TestCatalogCoverageAndExclusions(t *testing.T) {
	if len(generatedToolSpecs) < 170 {
		t.Fatalf("expected at least 170 tools, got %d", len(generatedToolSpecs))
	}

	excludedPaths := map[string]struct{}{
		"/api/v1/ws":                               {},
		"/api/v1/a2a/v0.3/tasks/stream":            {},
		"/api/v1/a2a/v0.3/tasks/:taskId/subscribe": {},
	}

	for _, spec := range generatedToolSpecs {
		if _, ok := excludedPaths[spec.Path]; ok {
			t.Fatalf("excluded route unexpectedly present in catalog: %s %s", spec.Method, spec.Path)
		}
	}

	mustExist := []struct {
		method string
		path   string
	}{
		{method: "POST", path: "/api/v1/auth/register"},
		{method: "POST", path: "/api/v1/auth/login"},
		{method: "GET", path: "/api/v1/auth/me"},
		{method: "GET", path: "/api/v1/admin/local-emails"},
		{method: "GET", path: "/api/v1/ws/stats"},
		{method: "POST", path: "/api/v1/invoices/:id/send"},
	}
	for _, expected := range mustExist {
		if _, ok := findToolSpec(expected.method, expected.path); !ok {
			t.Fatalf("expected route missing from catalog: %s %s", expected.method, expected.path)
		}
	}
}

func TestToolNamingDeterministic(t *testing.T) {
	spec, ok := findToolSpec("POST", "/api/v1/invoices/:id/send")
	if !ok {
		t.Fatal("missing POST /api/v1/invoices/:id/send")
	}
	if spec.Name != "post_invoices_by_id_send" {
		t.Fatalf("unexpected tool name: %s", spec.Name)
	}
}

func findToolSpec(method, path string) (ToolSpec, bool) {
	for _, spec := range generatedToolSpecs {
		if spec.Method == method && spec.Path == path {
			return spec, true
		}
	}
	return ToolSpec{}, false
}
