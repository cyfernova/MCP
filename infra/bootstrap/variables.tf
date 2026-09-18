variable "aws_region" {
  description = "AWS region for Terraform state resources."
  type        = string
}

variable "state_bucket_name" {
  description = "Globally unique S3 bucket name for Terraform state."
  type        = string
}
