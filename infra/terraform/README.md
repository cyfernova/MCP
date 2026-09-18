# AWS Lambda deployment

This deployment exposes the MCP HTTP service through an API Gateway HTTP API.
AWS terminates public TLS. The Lambda adapter preserves the application's JWT,
tool allowlist, schema validation, and rate-limit controls; direct server mTLS
does not apply at the API Gateway edge.

## Deploy

1. Copy `terraform.tfvars.example` to `terraform.tfvars` and set the backend
   and identity-provider values.
2. Create the ECR repository first:

   ```sh
   terraform init
   terraform apply -target=aws_ecr_repository.mcp -target=aws_ecr_lifecycle_policy.mcp
   ```

3. Authenticate Docker, build and push an immutable image tag:

   ```sh
   REPOSITORY=$(terraform output -raw ecr_repository_url)
   aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin "${REPOSITORY%/*}"
   docker build --platform linux/amd64 -f Dockerfile.lambda -t "$REPOSITORY:2026-09-18" .
   docker push "$REPOSITORY:2026-09-18"
   ```

4. Resolve the digest and set `image_uri` in `terraform.tfvars` to
   `repository-url@sha256:...`, then run `terraform apply`.

The `api_endpoint` output is the base URL; use `api_endpoint/mcp` for the
Streamable HTTP MCP endpoint and `api_endpoint/health` for health checks.
