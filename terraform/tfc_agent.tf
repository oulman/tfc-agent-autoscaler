# --- TFC Agent Fargate Service ---

resource "aws_cloudwatch_log_group" "tfc_agent" {
  name              = "/ecs/${var.name_prefix}-agent"
  retention_in_days = 30

  tags = var.tags
}

# --- Agent Execution Role (pull images, fetch agent token secret, write logs) ---

resource "aws_iam_role" "agent_execution" {
  name = "${var.name_prefix}-agent-execution"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "agent_execution_managed" {
  role       = aws_iam_role.agent_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "agent_execution_secrets" {
  name = "secrets-access"
  role = aws_iam_role.agent_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "secretsmanager:GetSecretValue"
      Resource = aws_secretsmanager_secret.tfc_agent_token.arn
    }]
  })
}

# --- Agent Task Role (minimal, extensible for Terraform AWS provider access) ---

resource "aws_iam_role" "agent_task" {
  name = "${var.name_prefix}-agent-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })

  tags = var.tags
}

# --- Agent Security Group ---

resource "aws_security_group" "tfc_agent" {
  name_prefix = "${var.name_prefix}-agent-"
  description = "Security group for the TFC agent Fargate tasks"
  vpc_id      = aws_vpc.main.id

  tags = merge(var.tags, { Name = "${var.name_prefix}-agent-sg" })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_egress_rule" "tfc_agent_all_outbound" {
  security_group_id = aws_security_group.tfc_agent.id
  description       = "Allow all outbound traffic (TFC connectivity, Terraform providers)"
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"

  tags = merge(var.tags, { Name = "${var.name_prefix}-agent-egress-all" })
}

# --- Agent Task Definition ---

resource "aws_ecs_task_definition" "tfc_agent" {
  family                   = "${var.name_prefix}-agent"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.tfc_agent_cpu
  memory                   = var.tfc_agent_memory
  execution_role_arn       = aws_iam_role.agent_execution.arn
  task_role_arn            = aws_iam_role.agent_task.arn

  container_definitions = jsonencode([{
    name      = "tfc-agent"
    image     = var.tfc_agent_image
    essential = true

    environment = [
      { name = "TFE_ADDRESS", value = var.tfc_address },
      { name = "TFC_AGENT_NAME", value = var.name_prefix },
    ]

    secrets = [{
      name      = "TFC_AGENT_TOKEN"
      valueFrom = aws_secretsmanager_secret.tfc_agent_token.arn
    }]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.tfc_agent.name
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "agent"
      }
    }
  }])

  tags = var.tags
}

# --- Agent ECS Service ---

resource "aws_ecs_service" "tfc_agent" {
  name            = "${var.name_prefix}-agent"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.tfc_agent.arn
  desired_count   = var.min_agents
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.tfc_agent.id]
    assign_public_ip = false
  }

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  tags = var.tags
}
