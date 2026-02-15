resource "aws_security_group" "autoscaler" {
  name_prefix = "${var.name_prefix}-"
  description = "Security group for the TFC agent autoscaler Fargate tasks"
  vpc_id      = aws_vpc.main.id

  tags = merge(var.tags, { Name = "${var.name_prefix}-sg" })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_egress_rule" "autoscaler_all_outbound" {
  security_group_id = aws_security_group.autoscaler.id
  description       = "Allow all outbound traffic (TFC API, ECS API, ECR)"
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"

  tags = merge(var.tags, { Name = "${var.name_prefix}-egress-all" })
}
