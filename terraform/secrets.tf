# --- Secrets Manager: TFC API token (used by the autoscaler) ---

resource "aws_secretsmanager_secret" "tfc_token" {
  name = "${var.name_prefix}-tfc-api-token"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "tfc_token" {
  secret_id     = aws_secretsmanager_secret.tfc_token.id
  secret_string = var.tfc_token
}

# --- Secrets Manager: TFC agent token (used by the tfc-agent service) ---

resource "aws_secretsmanager_secret" "tfc_agent_token" {
  name = "${var.name_prefix}-tfc-agent-token"

  tags = var.tags
}

resource "aws_secretsmanager_secret_version" "tfc_agent_token" {
  secret_id     = aws_secretsmanager_secret.tfc_agent_token.id
  secret_string = var.tfc_agent_token
}
