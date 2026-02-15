# --- ECR Pull-Through Cache for Docker Hub ---
#
# Caches Docker Hub images (e.g. hashicorp/tfc-agent) in ECR to avoid
# Docker Hub rate limits and reduce pull latency for Fargate tasks.

data "aws_caller_identity" "current" {}

resource "aws_ecr_pull_through_cache_rule" "docker_hub" {
  ecr_repository_prefix = var.ecr_cache_prefix
  upstream_registry_url = "registry-1.docker.io"

  depends_on = [aws_ecr_repository_creation_template.docker_hub]
}

resource "aws_ecr_repository_creation_template" "docker_hub" {
  prefix      = var.ecr_cache_prefix
  description = "Pull-through cache repos for Docker Hub images"
  applied_for = ["PULL_THROUGH_CACHE"]

  image_tag_mutability = "IMMUTABLE"

  encryption_configuration {
    encryption_type = "AES256"
  }

  resource_tags = var.tags
}
