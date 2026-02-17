package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type routeRecord struct {
	Method  string
	Path    string
	Handler string
	Family  string
}

type swaggerDoc struct {
	Paths       map[string]map[string]swaggerOperation `json:"paths"`
	Definitions map[string]any                         `json:"definitions"`
}

type swaggerOperation struct {
	Summary     string         `json:"summary"`
	Description string         `json:"description"`
	Parameters  []swaggerParam `json:"parameters"`
}

type swaggerParam struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Required    bool           `json:"required"`
	Type        string         `json:"type"`
	Format      string         `json:"format"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
	Enum        []any          `json:"enum"`
	Items       map[string]any `json:"items"`
	Minimum     *float64       `json:"minimum"`
	Maximum     *float64       `json:"maximum"`
}

type handlerMeta struct {
	BodyUsed    bool
	QueryParams map[string]string
	Headers     map[string]string
}

type generatedToolSpec struct {
	Name            string
	Description     string
	Method          string
	Path            string
	Family          string
	PathParams      []string
	InputSchemaJSON string
}

type schemaBuildInput struct {
	Method      string
	Path        string
	PathParams  []string
	SwaggerOp   *swaggerOperation
	HandlerMeta handlerMeta
}

func main() {
	mcpRootDefault := "."
	backendRootDefault := "../invoice-backend"

	mcpRoot := flag.String("mcp-root", mcpRootDefault, "path to mcp project root")
	backendRoot := flag.String("backend-root", backendRootDefault, "path to invoice-backend root")
	flag.Parse()

	absMCPRoot, err := filepath.Abs(*mcpRoot)
	if err != nil {
		fatalf("resolve mcp root: %v", err)
	}
	absBackendRoot, err := filepath.Abs(*backendRoot)
	if err != nil {
		fatalf("resolve backend root: %v", err)
	}

	routerPath := filepath.Join(absBackendRoot, "cmd", "api", "main.go")
	docsPath := filepath.Join(absBackendRoot, "docs", "docs.go")
	handlersDir := filepath.Join(absBackendRoot, "internal", "handlers")
	outPath := filepath.Join(absMCPRoot, "internal", "tools", "catalog_generated.go")

	routes, err := extractRoutes(routerPath)
	if err != nil {
		fatalf("extract routes: %v", err)
	}

	swagger, err := loadSwaggerFromDocs(docsPath)
	if err != nil {
		fatalf("load swagger: %v", err)
	}

	handlerFieldTypes, err := parseHandlerFieldTypes(filepath.Join(handlersDir, "handler.go"))
	if err != nil {
		fatalf("parse handler field types: %v", err)
	}

	handlerMetaByMethod, err := parseHandlerMetadata(handlersDir)
	if err != nil {
		fatalf("parse handler metadata: %v", err)
	}

	filtered := filterRoutes(routes)
	if len(filtered) == 0 {
		fatalf("no routes after filtering")
	}

	specs := make([]generatedToolSpec, 0, len(filtered))
	for _, route := range filtered {
		rel := strings.TrimPrefix(route.Path, "/api/v1")
		if rel == "" {
			rel = "/"
		}
		swaggerPath := colonPathToBraces(rel)
		var op *swaggerOperation
		if methods, ok := swagger.Paths[swaggerPath]; ok {
			if candidate, ok := methods[strings.ToLower(route.Method)]; ok {
				op = &candidate
			}
		}

		meta := lookupHandlerMeta(route.Handler, handlerFieldTypes, handlerMetaByMethod)
		pathParams := pathParamNames(route.Path)
		schema, err := buildToolInputSchema(schemaBuildInput{
			Method:      route.Method,
			Path:        route.Path,
			PathParams:  pathParams,
			SwaggerOp:   op,
			HandlerMeta: meta,
		})
		if err != nil {
			fatalf("build schema for %s %s: %v", route.Method, route.Path, err)
		}

		schemaJSON, err := json.Marshal(schema)
		if err != nil {
			fatalf("marshal schema for %s %s: %v", route.Method, route.Path, err)
		}

		description := strings.TrimSpace(route.Method + " " + route.Path)
		if op != nil {
			if s := strings.TrimSpace(op.Summary); s != "" {
				description = s
			} else if d := strings.TrimSpace(op.Description); d != "" {
				description = d
			}
		}

		specs = append(specs, generatedToolSpec{
			Name:            toolName(route.Method, route.Path),
			Description:     description,
			Method:          route.Method,
			Path:            route.Path,
			Family:          routeFamily(route.Path),
			PathParams:      pathParams,
			InputSchemaJSON: string(schemaJSON),
		})
	}

	sort.Slice(specs, func(i, j int) bool {
		if specs[i].Name != specs[j].Name {
			return specs[i].Name < specs[j].Name
		}
		if specs[i].Method != specs[j].Method {
			return specs[i].Method < specs[j].Method
		}
		return specs[i].Path < specs[j].Path
	})

	routerSourceBytes, err := os.ReadFile(routerPath)
	if err != nil {
		fatalf("read router source for hash: %v", err)
	}
	routerHash := sha256.Sum256(routerSourceBytes)
	routerHashHex := hex.EncodeToString(routerHash[:])

	swaggerDefsDoc := map[string]any{"definitions": rewriteRefs(swagger.Definitions)}
	swaggerDefsJSON, err := json.Marshal(swaggerDefsDoc)
	if err != nil {
		fatalf("marshal swagger definitions: %v", err)
	}

	generated := renderGeneratedFile(renderInput{
		GeneratedAt:            time.Now().UTC().Format(time.RFC3339),
		RouterFileHashSHA256:   routerHashHex,
		BackendRouterPath:      filepath.ToSlash(filepath.Join("..", "invoice-backend", "cmd", "api", "main.go")),
		SwaggerDefinitionsJSON: string(swaggerDefsJSON),
		Specs:                  specs,
	})

	if err := os.WriteFile(outPath, []byte(generated), 0o644); err != nil {
		fatalf("write generated catalog: %v", err)
	}

	fmt.Printf("generated %d tools -> %s\n", len(specs), outPath)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func extractRoutes(routerFile string) ([]routeRecord, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, routerFile, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse router file: %w", err)
	}

	var setupFunc *ast.FuncDecl
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != "setupRouter" {
			continue
		}
		setupFunc = fd
		break
	}
	if setupFunc == nil || setupFunc.Body == nil {
		return nil, errors.New("setupRouter function not found")
	}

	ex := &routeExtractor{
		fset:    fset,
		routes:  make([]routeRecord, 0),
		methods: map[string]struct{}{"GET": {}, "POST": {}, "PUT": {}, "DELETE": {}, "PATCH": {}},
	}
	env := map[string]string{"router": ""}
	ex.processBlock(setupFunc.Body, env)

	return ex.routes, nil
}

type routeExtractor struct {
	fset    *token.FileSet
	routes  []routeRecord
	methods map[string]struct{}
}

func (e *routeExtractor) processBlock(block *ast.BlockStmt, env map[string]string) {
	if block == nil {
		return
	}
	for _, stmt := range block.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			e.processAssign(s, env)
		case *ast.ExprStmt:
			e.processExpr(s.X, env)
		case *ast.BlockStmt:
			child := copyEnv(env)
			e.processBlock(s, child)
		case *ast.IfStmt:
			child := copyEnv(env)
			e.processBlock(s.Body, child)
			if elseBlock, ok := s.Else.(*ast.BlockStmt); ok {
				childElse := copyEnv(env)
				e.processBlock(elseBlock, childElse)
			}
		}
	}
}

func (e *routeExtractor) processAssign(assign *ast.AssignStmt, env map[string]string) {
	if len(assign.Lhs) != len(assign.Rhs) {
		return
	}

	for i := range assign.Lhs {
		lhs, ok := assign.Lhs[i].(*ast.Ident)
		if !ok {
			continue
		}
		call, ok := assign.Rhs[i].(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if sel.Sel == nil || sel.Sel.Name != "Group" {
			continue
		}
		base := e.resolvePrefix(sel.X, env)
		segment, ok := stringArg(call, 0)
		if !ok {
			continue
		}
		env[lhs.Name] = joinPath(base, segment)
	}
}

func (e *routeExtractor) processExpr(expr ast.Expr, env map[string]string) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return
	}
	method := strings.ToUpper(sel.Sel.Name)
	if _, ok := e.methods[method]; !ok {
		return
	}
	pathArg, ok := stringArg(call, 0)
	if !ok {
		return
	}
	base := e.resolvePrefix(sel.X, env)
	fullPath := joinPath(base, pathArg)
	handler := ""
	if len(call.Args) > 1 {
		handler = exprString(e.fset, call.Args[len(call.Args)-1])
	}
	e.routes = append(e.routes, routeRecord{
		Method:  method,
		Path:    fullPath,
		Handler: handler,
		Family:  routeFamily(fullPath),
	})
}

func (e *routeExtractor) resolvePrefix(expr ast.Expr, env map[string]string) string {
	switch x := expr.(type) {
	case *ast.Ident:
		if value, ok := env[x.Name]; ok {
			return value
		}
	case *ast.SelectorExpr:
		return e.resolvePrefix(x.X, env)
	}
	return ""
}

func stringArg(call *ast.CallExpr, index int) (string, bool) {
	if index >= len(call.Args) {
		return "", false
	}
	lit, ok := call.Args[index].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func exprString(fset *token.FileSet, expr ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, expr); err != nil {
		return ""
	}
	return buf.String()
}

func copyEnv(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func filterRoutes(routes []routeRecord) []routeRecord {
	excluded := map[string]struct{}{
		"GET /api/v1/ws":                               {},
		"POST /api/v1/a2a/v0.3/tasks/stream":           {},
		"GET /api/v1/a2a/v0.3/tasks/:taskId/subscribe": {},
	}

	result := make([]routeRecord, 0, len(routes))
	seen := make(map[string]struct{})
	for _, route := range routes {
		if !strings.HasPrefix(route.Path, "/api/v1") {
			continue
		}
		key := route.Method + " " + route.Path
		if _, ok := excluded[key]; ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, route)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Path != result[j].Path {
			return result[i].Path < result[j].Path
		}
		return result[i].Method < result[j].Method
	})
	return result
}

func loadSwaggerFromDocs(docsFile string) (*swaggerDoc, error) {
	src, err := os.ReadFile(docsFile)
	if err != nil {
		return nil, fmt.Errorf("read docs file: %w", err)
	}

	content := string(src)
	const prefix = "const docTemplate = `"
	start := strings.Index(content, prefix)
	if start == -1 {
		return nil, errors.New("docTemplate constant not found")
	}
	start += len(prefix)
	end := strings.Index(content[start:], "`")
	if end == -1 {
		return nil, errors.New("docTemplate closing backtick not found")
	}
	templateStr := content[start : start+end]

	replaced := templateStr
	replacements := map[string]string{
		"{{ marshal .Schemes }}":  `["https"]`,
		"{{escape .Description}}": "",
		"{{.Title}}":              "Invoice Backend API",
		"{{.Version}}":            "1.0.0",
		"{{.Host}}":               "localhost:8080",
		"{{.BasePath}}":           "/api/v1",
	}
	for old, newValue := range replacements {
		replaced = strings.ReplaceAll(replaced, old, newValue)
	}

	var doc swaggerDoc
	if err := json.Unmarshal([]byte(replaced), &doc); err != nil {
		return nil, fmt.Errorf("unmarshal rendered swagger doc: %w", err)
	}
	if doc.Paths == nil {
		doc.Paths = map[string]map[string]swaggerOperation{}
	}
	if doc.Definitions == nil {
		doc.Definitions = map[string]any{}
	}
	return &doc, nil
}

func parseHandlerFieldTypes(handlerFile string) (map[string]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, handlerFile, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name == nil || ts.Name.Name != "Handler" {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				continue
			}
			for _, field := range st.Fields.List {
				if len(field.Names) == 0 {
					continue
				}
				name := field.Names[0].Name
				typ := typeExprName(field.Type)
				if name != "" && typ != "" {
					result[name] = typ
				}
			}
		}
	}

	if len(result) == 0 {
		return nil, errors.New("handler field/type map is empty")
	}
	return result, nil
}

func typeExprName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return typeExprName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		if xIdent, ok := t.X.(*ast.Ident); ok {
			return xIdent.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	default:
		return ""
	}
}

func parseHandlerMetadata(handlersDir string) (map[string]handlerMeta, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, handlersDir, func(info os.FileInfo) bool {
		name := info.Name()
		if strings.HasSuffix(name, "_test.go") {
			return false
		}
		return strings.HasSuffix(name, ".go")
	}, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	pkg, ok := pkgs["handlers"]
	if !ok {
		return nil, errors.New("handlers package not found")
	}

	result := make(map[string]handlerMeta)
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name == nil || fd.Body == nil || len(fd.Recv.List) == 0 {
				continue
			}
			recvType := typeExprName(fd.Recv.List[0].Type)
			if recvType == "" {
				continue
			}
			meta := handlerMeta{
				QueryParams: map[string]string{},
				Headers:     map[string]string{},
			}

			ast.Inspect(fd.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil {
					return true
				}
				switch sel.Sel.Name {
				case "ShouldBindJSON", "BindJSON", "ShouldBindBodyWith", "ShouldBind":
					meta.BodyUsed = true
				case "Query", "DefaultQuery":
					if key, ok := firstStringArg(call); ok {
						meta.QueryParams[key] = "string"
					}
				case "GetHeader":
					if key, ok := firstStringArg(call); ok {
						meta.Headers[key] = "string"
					}
				}
				return true
			})

			result[recvType+"."+fd.Name.Name] = meta
		}
	}

	return result, nil
}

func firstStringArg(call *ast.CallExpr) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func lookupHandlerMeta(handlerExpr string, fieldTypes map[string]string, methodMeta map[string]handlerMeta) handlerMeta {
	parts := strings.Split(strings.TrimSpace(handlerExpr), ".")
	if len(parts) < 3 {
		return handlerMeta{QueryParams: map[string]string{}, Headers: map[string]string{}}
	}
	field := parts[len(parts)-2]
	method := parts[len(parts)-1]
	receiverType, ok := fieldTypes[field]
	if !ok {
		return handlerMeta{QueryParams: map[string]string{}, Headers: map[string]string{}}
	}
	meta, ok := methodMeta[receiverType+"."+method]
	if !ok {
		return handlerMeta{QueryParams: map[string]string{}, Headers: map[string]string{}}
	}
	if meta.QueryParams == nil {
		meta.QueryParams = map[string]string{}
	}
	if meta.Headers == nil {
		meta.Headers = map[string]string{}
	}
	return meta
}

func buildToolInputSchema(in schemaBuildInput) (map[string]any, error) {
	properties := map[string]any{}
	required := make([]string, 0)

	pathParamTypes := map[string]map[string]any{}
	queryParamSchemas := map[string]map[string]any{}
	headerParamSchemas := map[string]map[string]any{}
	queryRequired := make([]string, 0)
	headersRequired := make([]string, 0)

	var bodySchema map[string]any
	bodyRequired := false

	if in.SwaggerOp != nil {
		for _, p := range in.SwaggerOp.Parameters {
			schema := schemaForSwaggerParam(p)
			switch p.In {
			case "path":
				pathParamTypes[p.Name] = schema
			case "query":
				queryParamSchemas[p.Name] = schema
				if p.Required {
					queryRequired = append(queryRequired, p.Name)
				}
			case "header":
				headerParamSchemas[p.Name] = schema
				if p.Required {
					headersRequired = append(headersRequired, p.Name)
				}
			case "body":
				if p.Schema != nil {
					converted := rewriteRefs(p.Schema)
					if m, ok := converted.(map[string]any); ok {
						bodySchema = m
					}
				}
				bodyRequired = p.Required
			}
		}
	}

	if len(in.PathParams) > 0 {
		pathProps := map[string]any{}
		for _, name := range in.PathParams {
			if schema, ok := pathParamTypes[name]; ok {
				pathProps[name] = schema
			} else {
				pathProps[name] = map[string]any{"type": "string"}
			}
		}
		properties["path"] = map[string]any{
			"type":                 "object",
			"properties":           pathProps,
			"required":             in.PathParams,
			"additionalProperties": false,
		}
		required = append(required, "path")
	}

	if len(queryParamSchemas) == 0 {
		for key, typ := range in.HandlerMeta.QueryParams {
			normalized := normalizeType(typ)
			if normalized == "string" {
				queryParamSchemas[key] = inferredQueryValueSchema()
			} else {
				queryParamSchemas[key] = map[string]any{"type": normalized}
			}
		}
	}
	if len(queryParamSchemas) > 0 {
		schema := map[string]any{
			"type":                 "object",
			"properties":           queryParamSchemas,
			"additionalProperties": false,
		}
		if len(queryRequired) > 0 {
			sort.Strings(queryRequired)
			schema["required"] = dedupeSorted(queryRequired)
		}
		properties["query"] = schema
	}

	if len(headerParamSchemas) == 0 {
		for key, typ := range in.HandlerMeta.Headers {
			headerParamSchemas[key] = map[string]any{"type": normalizeType(typ)}
		}
	}
	if len(headerParamSchemas) > 0 {
		schema := map[string]any{
			"type":                 "object",
			"properties":           headerParamSchemas,
			"additionalProperties": false,
		}
		if len(headersRequired) > 0 {
			sort.Strings(headersRequired)
			schema["required"] = dedupeSorted(headersRequired)
		}
		properties["headers"] = schema
	}

	if bodySchema == nil && in.HandlerMeta.BodyUsed {
		bodySchema = map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		}
		bodyRequired = true
	}
	if bodySchema == nil && methodLikelyAllowsBody(in.Method) {
		bodySchema = map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
	if bodySchema != nil {
		properties["body"] = bodySchema
		if bodyRequired {
			required = append(required, "body")
		}
	}

	top := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		sort.Strings(required)
		top["required"] = dedupeSorted(required)
	}

	return top, nil
}

func dedupeSorted(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	last := ""
	for i, value := range values {
		if i == 0 || value != last {
			out = append(out, value)
		}
		last = value
	}
	return out
}

func methodLikelyAllowsBody(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

func inferredQueryValueSchema() map[string]any {
	scalar := []any{
		map[string]any{"type": "string"},
		map[string]any{"type": "integer"},
		map[string]any{"type": "number"},
		map[string]any{"type": "boolean"},
	}
	return map[string]any{
		"oneOf": []any{
			map[string]any{"anyOf": scalar},
			map[string]any{
				"type": "array",
				"items": map[string]any{
					"anyOf": scalar,
				},
			},
		},
	}
}

func schemaForSwaggerParam(param swaggerParam) map[string]any {
	if param.Schema != nil && len(param.Schema) > 0 {
		if converted, ok := rewriteRefs(param.Schema).(map[string]any); ok {
			return converted
		}
	}

	schema := map[string]any{"type": normalizeType(param.Type)}
	if len(param.Enum) > 0 {
		schema["enum"] = param.Enum
	}
	if param.Minimum != nil {
		schema["minimum"] = *param.Minimum
	}
	if param.Maximum != nil {
		schema["maximum"] = *param.Maximum
	}
	if normalizeType(param.Type) == "array" && param.Items != nil {
		schema["items"] = rewriteRefs(param.Items)
	}
	return schema
}

func normalizeType(t string) string {
	t = strings.TrimSpace(strings.ToLower(t))
	switch t {
	case "", "file":
		return "string"
	case "int", "int32", "int64", "integer":
		return "integer"
	case "float", "double", "number":
		return "number"
	case "bool", "boolean":
		return "boolean"
	case "array":
		return "array"
	case "object":
		return "object"
	default:
		return "string"
	}
}

func rewriteRefs(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if key == "$ref" {
				if ref, ok := item.(string); ok && strings.HasPrefix(ref, "#/definitions/") {
					out[key] = "mem://swagger-definitions.json" + ref
					continue
				}
			}
			out[key] = rewriteRefs(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = rewriteRefs(v[i])
		}
		return out
	default:
		return value
	}
}

func joinPath(base, segment string) string {
	base = strings.TrimSpace(base)
	segment = strings.TrimSpace(segment)

	if base == "" {
		if segment == "" {
			return "/"
		}
		if strings.HasPrefix(segment, "/") {
			return cleanPath(segment)
		}
		return cleanPath("/" + segment)
	}
	if segment == "" {
		return cleanPath(base)
	}
	if strings.HasPrefix(segment, "/") {
		return cleanPath(strings.TrimRight(base, "/") + segment)
	}
	return cleanPath(strings.TrimRight(base, "/") + "/" + segment)
}

func cleanPath(path string) string {
	if path == "" {
		return "/"
	}
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	if path == "" {
		return "/"
	}
	return path
}

func colonPathToBraces(path string) string {
	parts := strings.Split(path, "/")
	for i := range parts {
		if strings.HasPrefix(parts[i], ":") && len(parts[i]) > 1 {
			parts[i] = "{" + parts[i][1:] + "}"
		}
	}
	out := strings.Join(parts, "/")
	if out == "" {
		return "/"
	}
	if !strings.HasPrefix(out, "/") {
		out = "/" + out
	}
	return out
}

func pathParamNames(path string) []string {
	parts := strings.Split(path, "/")
	out := make([]string, 0)
	for _, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			out = append(out, part[1:])
		}
	}
	return out
}

func routeFamily(path string) string {
	rel := strings.TrimPrefix(path, "/api/v1")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "root"
	}
	segment := strings.Split(rel, "/")[0]
	if segment == "" {
		return "root"
	}
	return sanitizeToken(segment)
}

func toolName(method, path string) string {
	rel := strings.TrimPrefix(path, "/api/v1")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return strings.ToLower(method) + "_root"
	}

	parts := strings.Split(rel, "/")
	tokens := make([]string, 0, len(parts)+1)
	tokens = append(tokens, strings.ToLower(method))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if strings.HasPrefix(part, ":") {
			tokens = append(tokens, "by_"+sanitizeToken(part[1:]))
			continue
		}
		tokens = append(tokens, sanitizeToken(part))
	}
	return strings.Join(tokens, "_")
}

func sanitizeToken(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return "value"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "value"
	}
	return out
}

type renderInput struct {
	GeneratedAt            string
	RouterFileHashSHA256   string
	BackendRouterPath      string
	SwaggerDefinitionsJSON string
	Specs                  []generatedToolSpec
}

func renderGeneratedFile(in renderInput) string {
	var b strings.Builder
	b.WriteString("// Code generated by cmd/gen-catalog. DO NOT EDIT.\n")
	b.WriteString("package tools\n\n")
	b.WriteString("const GeneratedCatalogAt = ")
	b.WriteString(strconv.Quote(in.GeneratedAt))
	b.WriteString("\n")
	b.WriteString("const GeneratedRouterFileSHA256 = ")
	b.WriteString(strconv.Quote(in.RouterFileHashSHA256))
	b.WriteString("\n")
	b.WriteString("const GeneratedBackendRouterPath = ")
	b.WriteString(strconv.Quote(in.BackendRouterPath))
	b.WriteString("\n\n")
	b.WriteString("const generatedSwaggerDefinitionsJSON = `")
	b.WriteString(in.SwaggerDefinitionsJSON)
	b.WriteString("`\n\n")
	b.WriteString("var generatedToolSpecs = []ToolSpec{\n")
	for _, spec := range in.Specs {
		b.WriteString("\t{\n")
		b.WriteString("\t\tName: ")
		b.WriteString(strconv.Quote(spec.Name))
		b.WriteString(",\n")
		b.WriteString("\t\tDescription: ")
		b.WriteString(strconv.Quote(spec.Description))
		b.WriteString(",\n")
		b.WriteString("\t\tMethod: ")
		b.WriteString(strconv.Quote(spec.Method))
		b.WriteString(",\n")
		b.WriteString("\t\tPath: ")
		b.WriteString(strconv.Quote(spec.Path))
		b.WriteString(",\n")
		b.WriteString("\t\tFamily: ")
		b.WriteString(strconv.Quote(spec.Family))
		b.WriteString(",\n")
		b.WriteString("\t\tPathParams: []string{")
		for i, param := range spec.PathParams {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(param))
		}
		b.WriteString("},\n")
		b.WriteString("\t\tInputSchemaJSON: `")
		b.WriteString(spec.InputSchemaJSON)
		b.WriteString("`,\n")
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	return b.String()
}
