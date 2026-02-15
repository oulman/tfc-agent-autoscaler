resource "aws_ecs_cluster" "main" {
  name = var.name_prefix

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = var.tags
}

resource "aws_ecs_cluster_capacity_providers" "main" {
  count = var.enable_spot_service ? 1 : 0

  cluster_name = aws_ecs_cluster.main.name

  capacity_providers = ["FARGATE", "FARGATE_SPOT"]
}

resource "aws_cloudwatch_log_group" "autoscaler" {
  name              = "/ecs/${var.name_prefix}"
  retention_in_days = 30

  tags = var.tags
}

resource "aws_ecs_task_definition" "autoscaler" {
  family                   = var.name_prefix
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.container_cpu
  memory                   = var.container_memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn

  container_definitions = jsonencode([{
    name      = "autoscaler"
    image     = var.container_image
    essential = true

    portMappings = [{
      containerPort = 8080
      protocol      = "tcp"
    }]

    environment = concat([
      { name = "TFC_AGENT_POOL_ID", value = var.tfc_agent_pool_id },
      { name = "TFC_ORG", value = var.tfc_org },
      { name = "TFE_ADDRESS", value = var.tfc_address },
      { name = "ECS_CLUSTER", value = aws_ecs_cluster.main.name },
      { name = "ECS_SERVICE", value = aws_ecs_service.tfc_agent.name },
      { name = "MIN_AGENTS", value = tostring(var.min_agents) },
      { name = "MAX_AGENTS", value = tostring(var.max_agents) },
      { name = "POLL_INTERVAL", value = var.poll_interval },
      { name = "COOLDOWN_PERIOD", value = var.cooldown_period },
      ], var.enable_spot_service ? [
      { name = "ECS_SPOT_SERVICE", value = aws_ecs_service.tfc_agent_spot[0].name },
      { name = "SPOT_MIN_AGENTS", value = tostring(var.spot_min_agents) },
      { name = "SPOT_MAX_AGENTS", value = tostring(var.spot_max_agents) },
    ] : [])

    secrets = [{
      name      = "TFC_TOKEN"
      valueFrom = aws_secretsmanager_secret.tfc_token.arn
    }]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.autoscaler.name
        "awslogs-region"        = var.aws_region
        "awslogs-stream-prefix" = "autoscaler"
      }
    }

    healthCheck = {
      command     = ["CMD-SHELL", "wget -qO- http://localhost:8080/healthz || exit 1"]
      interval    = 30
      timeout     = 5
      retries     = 3
      startPeriod = 10
    }
  }])

  tags = var.tags
}

resource "aws_ecs_service" "autoscaler" {
  name            = var.name_prefix
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.autoscaler.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.autoscaler.id]
    assign_public_ip = false
  }

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  tags = var.tags
}
