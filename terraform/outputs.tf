output "vpc_id" {
  description = "ID of the VPC"
  value       = aws_vpc.main.id
}

output "private_subnet_ids" {
  description = "IDs of the private subnets"
  value       = aws_subnet.private[*].id
}

output "ecs_cluster_name" {
  description = "Name of the ECS cluster running the autoscaler"
  value       = aws_ecs_cluster.main.name
}

output "ecs_cluster_arn" {
  description = "ARN of the ECS cluster running the autoscaler"
  value       = aws_ecs_cluster.main.arn
}

output "ecs_service_name" {
  description = "Name of the autoscaler ECS service"
  value       = aws_ecs_service.autoscaler.name
}

output "task_role_arn" {
  description = "ARN of the ECS task IAM role (for reference when scoping agent cluster permissions)"
  value       = aws_iam_role.task.arn
}

output "security_group_id" {
  description = "ID of the autoscaler security group"
  value       = aws_security_group.autoscaler.id
}

output "log_group_name" {
  description = "CloudWatch log group for autoscaler logs"
  value       = aws_cloudwatch_log_group.autoscaler.name
}

# --- TFC Agent outputs ---

output "tfc_agent_service_name" {
  description = "Name of the TFC agent ECS service"
  value       = aws_ecs_service.tfc_agent.name
}

output "tfc_agent_security_group_id" {
  description = "ID of the TFC agent security group"
  value       = aws_security_group.tfc_agent.id
}

output "tfc_agent_task_role_arn" {
  description = "ARN of the agent task role (attach additional policies for Terraform AWS provider access)"
  value       = aws_iam_role.agent_task.arn
}

output "tfc_agent_log_group_name" {
  description = "CloudWatch log group for TFC agent logs"
  value       = aws_cloudwatch_log_group.tfc_agent.name
}
