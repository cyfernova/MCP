# AWS Lambda deployment

This deployment exposes the MCP HTTP service through an API Gateway HTTP API.
AWS terminates public TLS. The Lambda adapter preserves the application's JWT,
tool allowlist, schema validation, and rate-limit controls; direct server mTLS
does not apply at the API Gateway edge.

## GitHub Actions production deployment

Run **Bootstrap Terraform State** once from the Actions tab before enabling the
production deployment. Give it a globally unique S3 bucket name and a DynamoDB
lock-table name. It creates versioned, encrypted state storage and migrates its
own state into that bucket.

Configure the following repository or `production` environment variables:

| Variable | Purpose |
| --- | --- |
| `AWS_DEPLOY_ROLE_ARN` | IAM role ARN trusted by this GitHub repository through OIDC. |
| `AWS_REGION` | Region for all deployment resources. |
| `AWS_ACCOUNT_ID` | The 12-digit new AWS account ID; deployment fails if credentials belong to another account. |
| `TF_STATE_BUCKET` | S3 bucket created by the bootstrap workflow. |
| `TF_LOCK_TABLE` | DynamoDB table created by the bootstrap workflow. |
| `MCP_NAME` | Resource and ECR repository name; lowercase letters, digits, hyphens. |
| `MCP_BACKEND_BASE_URL` | Backend API base URL; API Gateway stage prefixes such as `/dev` are supported. |

The production job requires GitHub's `production` environment, so its protection
rules can require approval before Terraform applies. On every push to `main`, it
ensures ECR exists, pushes an `linux/amd64` Lambda image, resolves its digest,
and applies that immutable digest to Lambda.

The IAM role must allow the Terraform-managed AWS resources plus ECR push
operations. Its OIDC trust policy should restrict the GitHub subject to this
repository and the `production` environment. Follow GitHub's current
[AWS OIDC guidance](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws)
when creating the trust policy, since the subject claim format varies by
repository and environment configuration.

## Manual deploy

1. Copy `terraform.tfvars.example` to `terraform.tfvars` and set the backend
   values. This mode uses local Terraform state; use the
   GitHub Actions path above for production state management.
2. Create the ECR repository first:

   ```sh
   terraform init -backend=false
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
