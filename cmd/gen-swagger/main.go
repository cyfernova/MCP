package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cyfernova/mcp/internal/tools"
)

func main() {
	outFile := "swagger.json"
	if len(os.Args) > 1 {
		outFile = os.Args[1]
	}

	if err := run(outFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s\n", outFile)
}

func run(outFile string) error {
	registry, err := tools.NewRegistry()
	if err != nil {
		return fmt.Errorf("create registry: %w", err)
	}

	toolsList := registry.List()

	swagger := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "CyperNova MCP Server",
			"description": "Model Context Protocol (MCP) server providing a secure gateway to the invoice backend API. Exposes 100+ tools organized by domain (agents, invoices, customers, payments, etc.). All endpoints except /health require mTLS client certificate authentication.",
			"version":     "1.0.0",
			"contact":     map[string]string{"name": "CyperNova"},
		},
		"servers": []map[string]any{
			{
				"url":         "https://localhost:8080",
				"description": "Local development server",
			},
		},
		"paths": map[string]any{
			"/health": map[string]any{
				"get": map[string]any{
					"operationId": "healthCheck",
					"summary":     "Health check",
					"description": "Returns server health status. No authentication required.",
					"tags":        []string{"System"},
					"responses:": map[string]any{
						"200": map[string]any{
							"description": "Server is healthy",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"status":    map[string]string{"type": "string", "example": "ok"},
											"timestamp": map[string]string{"type": "string", "example": "2026-04-27T10:00:00Z"},
										},
									},
								},
							},
						},
					},
				},
			},
			"/mcp/tools/list": map[string]any{
				"post": map[string]any{
					"operationId": "listTools",
					"summary":     "List all available tools",
					"description": "Returns a list of all MCP tools available to the client. Requires mTLS authentication.",
					"tags":        []string{"Tools"},
					"security:": []any{
						map[string]any{"mtls": []any{}},
					},
					"responses:": map[string]any{
						"200": map[string]any{
							"description": "List of tools",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"tools": map[string]any{
												"type": "array",
												"items": map[string]any{
													"type": "object",
													"properties": map[string]any{
														"name":        map[string]string{"type": "string"},
														"description": map[string]string{"type": "string"},
														"schema":      map[string]any{"type": "object"},
													},
												},
											},
										},
									},
								},
							},
						},
						"401": map[string]any{
							"description": "mTLS certificate required",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Error"},
								},
							},
						},
					},
				},
			},
			"/mcp/tools/call": map[string]any{
				"post": map[string]any{
					"operationId": "callTool",
					"summary":     "Call an MCP tool",
					"description": "Executes a named tool with the provided arguments. Requires mTLS authentication. Most tools also require a valid JWT bearer token in the Authorization header.",
					"tags":        []string{"Tools"},
					"security:": []any{
						map[string]any{"mtls": []any{}},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/CallToolRequest"},
							},
						},
					},
					"responses:": map[string]any{
						"200": map[string]any{
							"description": "Tool execution result",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/CallToolResponse"},
								},
							},
						},
						"400": map[string]any{
							"description": "Invalid request",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Error"},
								},
							},
						},
						"401": map[string]any{
							"description": "Authentication failed",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Error"},
								},
							},
						},
						"404": map[string]any{
							"description": "Tool not found",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Error"},
								},
							},
						},
						"429": map[string]any{
							"description": "Rate limit exceeded",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Error"},
								},
							},
						},
					},
				},
			},
			"/mcp": map[string]any{
				"post": map[string]any{
					"operationId": "mcpProtocol",
					"summary":     "MCP protocol endpoint",
					"description": "Main MCP protocol handler for initialize, list_tools, and call_tool operations. Requires mTLS authentication.",
					"tags":        []string{"MCP Protocol"},
					"security:": []any{
						map[string]any{"mtls": []any{}},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"type": "object"},
							},
						},
					},
					"responses:": map[string]any{
						"200": map[string]any{
							"description": "MCP protocol response",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"type": "object"},
								},
							},
						},
						"401": map[string]any{
							"description": "mTLS certificate required",
						},
					},
				},
			},
			"/mcp/": map[string]any{
				"post": map[string]any{
					"operationId": "mcpProtocolSlash",
					"summary":     "MCP protocol endpoint (trailing slash)",
					"description": "MCP protocol handler for tool calls. Requires mTLS authentication.",
					"tags":        []string{"MCP Protocol"},
					"security:": []any{
						map[string]any{"mtls": []any{}},
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"type": "object"},
							},
						},
					},
					"responses:": map[string]any{
						"200": map[string]any{
							"description": "MCP protocol response",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"type": "object"},
								},
							},
						},
						"401": map[string]any{
							"description": "mTLS certificate required",
						},
					},
				},
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"Error": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"error": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"code":       map[string]string{"type": "string", "description": "Machine-readable error code"},
								"message":    map[string]string{"type": "string", "description": "Human-readable error message"},
								"request_id": map[string]string{"type": "string", "description": "Unique request identifier for debugging"},
								"details":    map[string]any{"type": "object", "description": "Additional error context"},
							},
							"required": []string{"code", "message"},
						},
					},
				},
				"CallToolRequest": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tool": map[string]any{
							"type":        "string",
							"description": "Name of the tool to call (from the tools list)",
						},
						"args": map[string]any{
							"type":                 "object",
							"description":          "Tool-specific arguments as JSON object",
							"additionalProperties": true,
						},
					},
					"required": []string{"tool"},
				},
				"CallToolResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"result": map[string]any{
							"type":        "object",
							"description": "Tool execution result from backend",
						},
						"status": map[string]any{
							"type":        "integer",
							"description": "HTTP status code from backend",
						},
						"request_id": map[string]string{
							"type":        "string",
							"description": "Unique request identifier",
						},
						"backend_route": map[string]string{
							"type":        "string",
							"description": "Backend API route that was called",
						},
					},
				},
			},
			"securitySchemes": map[string]any{
				"mtls": map[string]any{
					"type":        "apiKey",
					"name":        "X-Client-Cert",
					"in":          "header",
					"description": "mTLS client certificate. All /mcp/* endpoints require a valid client certificate signed by the internal CA.",
				},
				"bearerAuth": map[string]any{
					"type":        "http",
					"scheme":      "bearer",
					"bearerFormat": "JWT",
					"description":  "JWT bearer token issued by the backend. Most tools require this unless the route is in the public auth routes list.",
				},
			},
		},
	}

	// Group tools by family
	toolsByFamily := groupByFamily(toolsList)
	families := make([]string, 0, len(toolsByFamily))
	for family := range toolsByFamily {
		families = append(families, family)
	}
	sort.Strings(families)

	// Build tool tag descriptions
	toolFamilies := map[string]string{
		"agents":            "Agent management - create, read, update, delete agents with various capabilities",
		"invoices":          "Invoice management - create, list, send, and manage invoices",
		"customers":         "Customer management - manage customer records and export",
		"payments":          "Payment processing - record and manage payments",
		"products":          "Product catalog management",
		"vendors":           "Vendor/supplier management",
		"business_profiles": "Business profile management",
		"teams":             "Team member management and invitations",
		"webhooks":          "Webhook configuration and events",
		"workflows":         "Workflow automation",
		"a2a":               "Agent-to-agent communication protocol v0.3",
		"a2a_bargaining":    "A2A bargaining sessions for price negotiations",
		"bargaining":        "Bargaining/negotiation tools",
		"discovery":        "Agent discovery registry",
		"ledger":            "Ledger entries and balance",
		"subscriptions":     "Subscription and plan management",
		"auth":              "Authentication - login, register, token refresh",
		"ws":                "WebSocket notifications",
	}

	// Add tags for each family
	tags := make([]map[string]any, 0, len(families))
	for _, family := range families {
		desc, ok := toolFamilies[family]
		if !ok {
			desc = fmt.Sprintf("Tools for %s operations", family)
		}
		tags = append(tags, map[string]any{
			"name":        family,
			"description": desc,
		})
	}
	swagger["tags"] = tags

	// Add tool paths
	paths := swagger["paths"].(map[string]any)

	for _, family := range families {
		specs := toolsByFamily[family]
		for _, spec := range specs {
			toolName := spec.Name
			path := "/" + strings.ReplaceAll(spec.Path, "/", "_")

			// Parse input schema
			var inputSchema map[string]any
			if spec.InputSchema != nil {
				inputSchema = spec.InputSchema
			}
			if inputSchema == nil {
				inputSchema = map[string]any{"type": "object"}
			}

			// Convert method to lowercase for OpenAPI
			method := strings.ToLower(spec.Method)

			opSummary := spec.Description
			if idx := strings.Index(opSummary, "DELETE "); idx == 0 {
				opSummary = "Delete " + spec.Path
			} else if idx := strings.Index(opSummary, "GET "); idx == 0 {
				opSummary = "Get " + spec.Path
			} else if idx := strings.Index(opSummary, "POST "); idx == 0 {
				opSummary = "Create " + spec.Path
			} else if idx := strings.Index(opSummary, "PUT "); idx == 0 {
				opSummary = "Update " + spec.Path
			} else if idx := strings.Index(opSummary, "PATCH "); idx == 0 {
				opSummary = "Patch " + spec.Path
			}

			requiresAuth := !isPublicRoute(spec.Method, spec.Path)

			var security []any
			if requiresAuth {
				security = []any{map[string]any{"bearerAuth": []any{}}}
			}

			// Build operation
			operation := map[string]any{
				"operationId": toolName,
				"summary":     opSummary,
				"description": fmt.Sprintf("Forwards to backend: %s %s\n\n**Family:** %s\n\n**Requires Auth:** %t", spec.Method, spec.Path, family, requiresAuth),
				"tags":        []string{family},
				"security":    security,
				"parameters":  buildParameters(inputSchema, spec.PathParams),
				"responses:": map[string]any{
					"200": map[string]any{
						"description": "Successful response from backend",
					},
					"400": map[string]any{
						"description": "Invalid arguments",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/Error"},
							},
						},
					},
					"401": map[string]any{
						"description": "Authentication required",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/Error"},
							},
						},
					},
					"429": map[string]any{
						"description": "Rate limit exceeded",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/Error"},
							},
						},
					},
					"502": map[string]any{
						"description": "Backend error or unavailable",
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{"$ref": "#/components/schemas/Error"},
							},
						},
					},
				},
			}

			if rb := buildRequestBody(inputSchema); rb != nil {
				operation["requestBody"] = rb
			}

			paths[path] = map[string]any{
				method: operation,
			}
		}
	}

	// Write output file
	f, err := os.Create(outFile)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(swagger)
}

func groupByFamily(specs []tools.Definition) map[string][]tools.Definition {
	result := make(map[string][]tools.Definition)
	for _, spec := range specs {
		result[spec.Family] = append(result[spec.Family], spec)
	}
	return result
}

func isPublicRoute(method, path string) bool {
	publicRoutes := map[string]bool{
		"POST /api/v1/auth/register":             true,
		"POST /api/v1/auth/login":                true,
		"POST /api/v1/auth/logout":               true,
		"POST /api/v1/auth/refresh":              true,
		"POST /api/v1/auth/forgot-password":      true,
		"POST /api/v1/auth/reset-password":       true,
		"POST /api/v1/auth/verify-email":         true,
		"POST /api/v1/auth/resend-verification":  true,
	}
	return publicRoutes[method+" "+path]
}

func buildParameters(inputSchema map[string]any, pathParams []string) []any {
	params := make([]any, 0)

	if props, ok := inputSchema["properties"].(map[string]any); ok {
		// Path params
		for _, name := range pathParams {
			if p, ok := props["path"].(map[string]any); ok {
				if pp, ok := p["properties"].(map[string]any); ok {
					if _, ok := pp[name].(map[string]any); ok {
						params = append(params, map[string]any{
							"name":        name,
							"in":          "path",
							"required":    true,
							"description": fmt.Sprintf("Path parameter: %s", name),
							"schema":      map[string]any{"type": "string"},
						})
					}
				}
			}
		}

		// Query params
		if q, ok := props["query"].(map[string]any); ok {
			if pp, ok := q["properties"].(map[string]any); ok {
				for name, prop := range pp {
					if pmap, ok := prop.(map[string]any); ok {
						params = append(params, map[string]any{
							"name":        name,
							"in":          "query",
							"required":    false,
							"description": fmt.Sprintf("Query parameter: %s", name),
							"schema":      pmap,
						})
					}
				}
			}
		}
	}

	return params
}

func buildRequestBody(inputSchema map[string]any) any {
	if props, ok := inputSchema["properties"].(map[string]any); ok {
		if body, ok := props["body"]; ok {
			if bmap, ok := body.(map[string]any); ok {
				return map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": bmap,
						},
					},
				}
			}
		}
	}
	return nil
}
