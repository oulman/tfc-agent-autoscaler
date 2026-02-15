variable "aws_region" {
  description = "AWS region to deploy into"
  type        = string
  default     = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix for all resource names"
  type        = string
  default     = "tfc-autoscaler"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "az_count" {
  description = "Number of availability zones to use"
  type        = number
  default     = 2
}

# --- Container configuration ---

variable "container_image" {
  description = "Docker image for the autoscaler (e.g. 123456789012.dkr.ecr.us-east-1.amazonaws.com/tfc-agent-autoscaler:latest)"
  type        = string
}

variable "container_cpu" {
  description = "Fargate task CPU units (256 = 0.25 vCPU)"
  type        = number
  default     = 256
}

variable "container_memory" {
  description = "Fargate task memory in MiB"
  type        = number
  default     = 512
}

# --- Autoscaler application configuration ---

variable "tfc_token" {
  description = "Terraform Cloud API token used by the autoscaler to query agent pool status"
  type        = string
  sensitive   = true
}

variable "tfc_agent_pool_id" {
  description = "Terraform Cloud agent pool ID to monitor"
  type        = string
}

variable "tfc_org" {
  description = "Terraform Cloud organization name"
  type        = string
}

variable "tfc_address" {
  description = "Terraform Cloud/Enterprise API address"
  type        = string
  default     = "https://app.terraform.io"
}

variable "min_agents" {
  description = "Minimum number of TFC agents"
  type        = number
  default     = 0
}

variable "max_agents" {
  description = "Maximum number of TFC agents"
  type        = number
  default     = 10
}

variable "poll_interval" {
  description = "How often the autoscaler reconciles (Go duration string)"
  type        = string
  default     = "10s"
}

variable "cooldown_period" {
  description = "Minimum time between scale-down events (Go duration string)"
  type        = string
  default     = "60s"
}

# --- TFC Agent service configuration ---

variable "tfc_agent_image" {
  description = "Docker image for the TFC agent"
  type        = string
  default     = "hashicorp/tfc-agent:latest"
}

variable "tfc_agent_token" {
  description = "Terraform Cloud agent token used by tfc-agent to register with the agent pool"
  type        = string
  sensitive   = true
}

variable "tfc_agent_cpu" {
  description = "Fargate task CPU units for the TFC agent (256 = 0.25 vCPU)"
  type        = number
  default     = 256
}

variable "tfc_agent_memory" {
  description = "Fargate task memory in MiB for the TFC agent"
  type        = number
  default     = 512
}

variable "tags" {
  description = "Tags to apply to all resources"
  type        = map(string)
  default     = {}
}
