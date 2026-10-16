resource "aws_vpc" "sandbox" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${local.name_prefix}-vpc" }
}

resource "aws_internet_gateway" "sandbox" {
  vpc_id = aws_vpc.sandbox.id

  tags = { Name = "${local.name_prefix}-igw" }
}

resource "aws_subnet" "public" {
  for_each = local.public_subnet_specs

  vpc_id                  = aws_vpc.sandbox.id
  availability_zone       = each.key
  cidr_block              = each.value.cidr
  map_public_ip_on_launch = false

  tags = {
    Name                                         = "${local.name_prefix}-public-${each.key}"
    "kubernetes.io/cluster/${local.name_prefix}" = "shared"
  }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.sandbox.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.sandbox.id
  }

  tags = { Name = "${local.name_prefix}-public" }
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}

resource "aws_security_group" "nodes" {
  name                   = "${local.name_prefix}-nodes"
  description            = "Restricted EKS worker security group for the learning sandbox"
  vpc_id                 = aws_vpc.sandbox.id
  revoke_rules_on_delete = true
  ingress                = []
  egress                 = []

  tags = { Name = "${local.name_prefix}-nodes" }
}

resource "aws_security_group" "endpoints" {
  name                   = "${local.name_prefix}-endpoints"
  description            = "Private ECR and EC2 API endpoint access from the worker node"
  vpc_id                 = aws_vpc.sandbox.id
  revoke_rules_on_delete = true
  ingress                = []
  egress                 = []

  tags = { Name = "${local.name_prefix}-endpoints" }
}

resource "aws_vpc_security_group_ingress_rule" "node_from_control_plane_kubelet" {
  security_group_id            = aws_security_group.nodes.id
  referenced_security_group_id = aws_eks_cluster.sandbox.vpc_config[0].cluster_security_group_id
  description                  = "EKS control plane to kubelet"
  from_port                    = 10250
  to_port                      = 10250
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "node_from_control_plane_https" {
  security_group_id            = aws_security_group.nodes.id
  referenced_security_group_id = aws_eks_cluster.sandbox.vpc_config[0].cluster_security_group_id
  description                  = "EKS control plane to node HTTPS endpoints"
  from_port                    = 443
  to_port                      = 443
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "node_to_control_plane" {
  security_group_id            = aws_security_group.nodes.id
  referenced_security_group_id = aws_eks_cluster.sandbox.vpc_config[0].cluster_security_group_id
  description                  = "Worker to private EKS API endpoint"
  from_port                    = 443
  to_port                      = 443
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "node_to_aws_endpoints" {
  security_group_id            = aws_security_group.nodes.id
  referenced_security_group_id = aws_security_group.endpoints.id
  description                  = "Worker to ECR and EC2 interface endpoints"
  from_port                    = 443
  to_port                      = 443
  ip_protocol                  = "tcp"
}

resource "aws_vpc_security_group_egress_rule" "node_to_s3" {
  security_group_id = aws_security_group.nodes.id
  description       = "Worker image-layer access to S3 through the gateway endpoint"
  prefix_list_id    = data.aws_prefix_list.s3.id
  from_port         = 443
  to_port           = 443
  ip_protocol       = "tcp"
}

resource "aws_vpc_security_group_ingress_rule" "endpoint_from_nodes" {
  security_group_id            = aws_security_group.endpoints.id
  referenced_security_group_id = aws_security_group.nodes.id
  description                  = "HTTPS from the EKS worker node"
  from_port                    = 443
  to_port                      = 443
  ip_protocol                  = "tcp"
}

resource "aws_vpc_endpoint" "ecr_api" {
  vpc_id              = aws_vpc.sandbox.id
  service_name        = "com.amazonaws.${local.region}.ecr.api"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.public[local.worker_az].id]
  security_group_ids  = [aws_security_group.endpoints.id]
  private_dns_enabled = true

  tags = { Name = "${local.name_prefix}-ecr-api" }
}

resource "aws_vpc_endpoint" "ecr_dkr" {
  vpc_id              = aws_vpc.sandbox.id
  service_name        = "com.amazonaws.${local.region}.ecr.dkr"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.public[local.worker_az].id]
  security_group_ids  = [aws_security_group.endpoints.id]
  private_dns_enabled = true

  tags = { Name = "${local.name_prefix}-ecr-dkr" }
}

resource "aws_vpc_endpoint" "ec2" {
  vpc_id              = aws_vpc.sandbox.id
  service_name        = "com.amazonaws.${local.region}.ec2"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = [aws_subnet.public[local.worker_az].id]
  security_group_ids  = [aws_security_group.endpoints.id]
  private_dns_enabled = true

  tags = { Name = "${local.name_prefix}-ec2-api" }
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.sandbox.id
  service_name      = "com.amazonaws.${local.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.public.id]

  tags = { Name = "${local.name_prefix}-s3" }
}
