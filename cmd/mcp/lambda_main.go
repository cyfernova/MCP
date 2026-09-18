//go:build lambda

package main

import (
	"context"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"

	"github.com/cyfernova/mcp/internal/config"
)

// main starts the API Gateway/Lambda variant of the MCP HTTP service.
func main() {
	cfg, err := config.LoadForLambda()
	if err != nil {
		exitWithConfigError(err)
	}

	log := newLogger(cfg.LogLevel)
	a, err := newApp(context.Background(), cfg, log)
	if err != nil {
		log.Error("failed to initialize Lambda application", "error", err)
		os.Exit(1)
	}

	lambda.Start(httpadapter.New(a.newHTTPHandler(false)).ProxyWithContext)
}
