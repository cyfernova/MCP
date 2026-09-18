output "api_endpoint" {
  description = "Public API Gateway endpoint. Append /mcp for Streamable HTTP MCP."
  value       = aws_apigatewayv2_stage.default.invoke_url
}

output "ecr_repository_url" {
  description = "ECR repository to which Docker images should be pushed."
  value       = aws_ecr_repository.mcp.repository_url
}

output "lambda_function_name" {
  value = aws_lambda_function.mcp.function_name
}
