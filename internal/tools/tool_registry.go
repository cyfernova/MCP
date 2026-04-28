package tools

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

const swaggerDefinitionsURL = "mem://swagger-definitions.json"

var (
	ErrToolNotFound   = errors.New("tool not found")
	ErrInvalidToolArg = errors.New("invalid tool arguments")

	blockedToolHeaders = map[string]struct{}{
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
)

// Definition is an allowlisted MCP tool mapped to a fixed backend route.
type Definition struct {
	Name        string
	Description string
	Method      string
	Path        string
	Family      string
	PathParams  []string
	InputSchema map[string]any
}

// PreparedCall is a backend call derived from a validated tool request.
type PreparedCall struct {
	Method       string
	Path         string
	Query        url.Values
	JSONBody     []byte
	Headers      map[string]string
	BackendRoute string
	RouteFamily  string
}

type compiledTool struct {
	spec      ToolSpec
	schemaMap map[string]any
	validator *jsonschema.Schema
}

// Registry stores all allowlisted tools.
type Registry struct {
	definitions map[string]*compiledTool
	ordered     []*compiledTool
}

func NewRegistry() (*Registry, error) {
	if len(generatedToolSpecs) == 0 {
		return nil, errors.New("generated tool catalog is empty")
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(swaggerDefinitionsURL, strings.NewReader(generatedSwaggerDefinitionsJSON)); err != nil {
		return nil, fmt.Errorf("add swagger definitions schema: %w", err)
	}

	r := &Registry{
		definitions: make(map[string]*compiledTool, len(generatedToolSpecs)),
		ordered:     make([]*compiledTool, 0, len(generatedToolSpecs)),
	}

	for _, spec := range generatedToolSpecs {
		if spec.Name == "" {
			return nil, errors.New("generated tool spec has empty name")
		}
		schemaURI := fmt.Sprintf("mem://tool/%s.json", spec.Name)
		if err := compiler.AddResource(schemaURI, strings.NewReader(spec.InputSchemaJSON)); err != nil {
			return nil, fmt.Errorf("add schema for tool %s: %w", spec.Name, err)
		}

		validator, err := compiler.Compile(schemaURI)
		if err != nil {
			return nil, fmt.Errorf("compile schema for tool %s: %w", spec.Name, err)
		}

		var schemaMap map[string]any
		if err := json.Unmarshal([]byte(spec.InputSchemaJSON), &schemaMap); err != nil {
			return nil, fmt.Errorf("decode schema map for tool %s: %w", spec.Name, err)
		}

		tool := &compiledTool{
			spec:      spec,
			schemaMap: schemaMap,
			validator: validator,
		}
		r.definitions[spec.Name] = tool
		r.ordered = append(r.ordered, tool)
	}

	sort.Slice(r.ordered, func(i, j int) bool {
		return r.ordered[i].spec.Name < r.ordered[j].spec.Name
	})

	return r, nil
}

func (r *Registry) List() []Definition {
	out := make([]Definition, 0, len(r.ordered))
	for _, tool := range r.ordered {
		out = append(out, Definition{
			Name:        tool.spec.Name,
			Description: tool.spec.Description,
			Method:      tool.spec.Method,
			Path:        tool.spec.Path,
			Family:      tool.spec.Family,
			PathParams:  tool.spec.PathParams,
			InputSchema: deepCopyMap(tool.schemaMap),
		})
	}
	return out
}

func (r *Registry) Prepare(toolName string, rawArgs json.RawMessage) (PreparedCall, error) {
	tool, ok := r.definitions[toolName]
	if !ok {
		return PreparedCall{}, fmt.Errorf("%w: %s", ErrToolNotFound, toolName)
	}

	argsMap, err := validateAndDecodeArgs(tool, rawArgs)
	if err != nil {
		return PreparedCall{}, err
	}

	resolvedPath, err := resolvePath(tool.spec.Path, tool.spec.PathParams, argsMap)
	if err != nil {
		return PreparedCall{}, err
	}

	queryValues, err := buildQuery(argsMap)
	if err != nil {
		return PreparedCall{}, err
	}

	bodyBytes, err := buildBody(argsMap)
	if err != nil {
		return PreparedCall{}, err
	}

	headers, err := buildHeaders(argsMap)
	if err != nil {
		return PreparedCall{}, err
	}

	return PreparedCall{
		Method:       tool.spec.Method,
		Path:         resolvedPath,
		Query:        queryValues,
		JSONBody:     bodyBytes,
		Headers:      headers,
		BackendRoute: tool.spec.Method + " " + resolvedPath,
		RouteFamily:  tool.spec.Family,
	}, nil
}

func validateAndDecodeArgs(tool *compiledTool, rawArgs json.RawMessage) (map[string]any, error) {
	payload := bytes.TrimSpace(rawArgs)
	if len(payload) == 0 || bytes.Equal(payload, []byte("null")) {
		payload = []byte("{}")
	}

	var generic any
	if err := json.Unmarshal(payload, &generic); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON arguments: %v", ErrInvalidToolArg, err)
	}

	if err := tool.validator.Validate(generic); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToolArg, err)
	}

	obj, ok := generic.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: tool args must be an object", ErrInvalidToolArg)
	}

	return obj, nil
}

func resolvePath(template string, pathParams []string, args map[string]any) (string, error) {
	if len(pathParams) == 0 {
		return template, nil
	}

	pathSection, ok := args["path"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("%w: path parameters are required", ErrInvalidToolArg)
	}

	resolved := template
	for _, param := range pathParams {
		value, ok := pathSection[param]
		if !ok {
			return "", fmt.Errorf("%w: missing path.%s", ErrInvalidToolArg, param)
		}
		resolved = strings.ReplaceAll(resolved, ":"+param, url.PathEscape(stringifyScalar(value)))
	}
	return resolved, nil
}

func buildQuery(args map[string]any) (url.Values, error) {
	values := url.Values{}
	querySection, ok := args["query"]
	if !ok {
		return values, nil
	}

	queryMap, ok := querySection.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: query must be an object", ErrInvalidToolArg)
	}
	for key, value := range queryMap {
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				values.Add(key, stringifyScalar(item))
			}
		default:
			values.Set(key, stringifyScalar(v))
		}
	}
	return values, nil
}

func buildBody(args map[string]any) ([]byte, error) {
	bodyValue, ok := args["body"]
	if !ok || bodyValue == nil {
		return nil, nil
	}
	body, err := json.Marshal(bodyValue)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal body: %v", ErrInvalidToolArg, err)
	}
	return body, nil
}

func buildHeaders(args map[string]any) (map[string]string, error) {
	headersSection, ok := args["headers"]
	if !ok {
		return map[string]string{}, nil
	}
	headersMap, ok := headersSection.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: headers must be an object", ErrInvalidToolArg)
	}

	out := make(map[string]string, len(headersMap))
	for key, value := range headersMap {
		rawKey := strings.TrimSpace(key)
		if rawKey == "" || !isValidHeaderName(rawKey) {
			return nil, fmt.Errorf("%w: header name %q is invalid", ErrInvalidToolArg, key)
		}

		canonical := http.CanonicalHeaderKey(rawKey)
		if canonical == "" {
			return nil, fmt.Errorf("%w: header name is required", ErrInvalidToolArg)
		}
		if _, blocked := blockedToolHeaders[canonical]; blocked {
			return nil, fmt.Errorf("%w: header %q is not allowed", ErrInvalidToolArg, canonical)
		}

		stringValue := strings.TrimSpace(stringifyScalar(value))
		if hasControlChars(stringValue) {
			return nil, fmt.Errorf("%w: header %q contains invalid characters", ErrInvalidToolArg, canonical)
		}
		out[canonical] = stringValue
	}
	return out, nil
}

func stringifyScalar(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(encoded)
	}
}

func deepCopyMap(in map[string]any) map[string]any {
	data, err := json.Marshal(in)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
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
