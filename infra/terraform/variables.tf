variable "aws_region" {
  description = "AWS region in which to create the MCP service."
  type        = string
}

variable "expected_aws_account_id" {
  description = "Twelve-digit AWS account ID authorized for this production deployment."
  type        = string

  validation {
    condition     = can(regex("^[0-9]{12}$", var.expected_aws_account_id))
    error_message = "expected_aws_account_id must be a 12-digit AWS account ID."
  }
}

variable "name" {
  description = "Short, unique name used as the resource prefix."
  type        = string
  default     = "cyfernova-mcp"

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9-]{0,62}$", var.name))
    error_message = "name must contain only lowercase letters, digits, and hyphens."
  }
}

variable "image_uri" {
  description = "Immutable ECR image URI for the Lambda image, for example repository-url@sha256:..."
  type        = string
}

variable "backend_base_url" {
  description = "HTTPS base URL of the backend API proxied by MCP. May include an API Gateway stage prefix, such as /dev."
  type        = string
}

variable "lambda_memory_mb" {
  description = "Lambda memory allocation in MB."
  type        = number
  default     = 512
}

variable "lambda_timeout_seconds" {
  description = "Lambda invocation timeout. API Gateway HTTP APIs allow at most 29 seconds."
  type        = number
  default     = 29

  validation {
    condition     = var.lambda_timeout_seconds >= 1 && var.lambda_timeout_seconds <= 29
    error_message = "lambda_timeout_seconds must be between 1 and 29."
  }
}

variable "rate_limit_rps" {
  description = "Per-warm-Lambda-instance request rate limit."
  type        = number
  default     = 5
}

variable "rate_limit_burst" {
  description = "Per-warm-Lambda-instance request burst limit."
  type        = number
  default     = 10
}

variable "log_retention_days" {
  description = "CloudWatch log retention period."
  type        = number
  default     = 30
}

variable "tags" {
  description = "Tags applied to all supported resources."
  type        = map(string)
  default = {
    Application = "cyfernova-mcp"
    ManagedBy   = "terraform"
  }
}
