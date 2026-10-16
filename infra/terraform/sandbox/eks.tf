data "aws_eks_addon_version" "vpc_cni" {
  addon_name         = "vpc-cni"
  kubernetes_version = var.kubernetes_version
  most_recent        = true
}

data "aws_eks_addon_version" "kube_proxy" {
  addon_name         = "kube-proxy"
  kubernetes_version = var.kubernetes_version
  most_recent        = true
}

data "aws_eks_addon_version" "coredns" {
  addon_name         = "coredns"
  kubernetes_version = var.kubernetes_version
  most_recent        = true
}

resource "aws_iam_role" "cluster" {
  name = "${local.name_prefix}-cluster"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "eks.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "cluster" {
  role       = aws_iam_role.cluster.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonEKSClusterPolicy"
}

resource "aws_eks_cluster" "sandbox" {
  name     = local.name_prefix
  role_arn = aws_iam_role.cluster.arn
  version  = var.kubernetes_version

  access_config {
    authentication_mode                         = "API"
    bootstrap_cluster_creator_admin_permissions = false
  }

  vpc_config {
    subnet_ids              = [for az in local.availability_zones : aws_subnet.public[az].id]
    endpoint_private_access = true
    endpoint_public_access  = true
    public_access_cidrs     = var.allowed_operator_cidrs
  }

  depends_on = [aws_iam_role_policy_attachment.cluster]

  lifecycle {
    precondition {
      condition     = split(":", var.operator_iam_principal_arn)[4] == data.aws_caller_identity.current.account_id
      error_message = "The operator IAM principal must belong to the AWS account used by this sandbox provider."
    }
  }

  tags = { Name = local.name_prefix }
}

resource "aws_eks_addon" "vpc_cni" {
  cluster_name                = aws_eks_cluster.sandbox.name
  addon_name                  = "vpc-cni"
  addon_version               = data.aws_eks_addon_version.vpc_cni.version
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "PRESERVE"
}

resource "aws_eks_addon" "kube_proxy" {
  cluster_name                = aws_eks_cluster.sandbox.name
  addon_name                  = "kube-proxy"
  addon_version               = data.aws_eks_addon_version.kube_proxy.version
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "PRESERVE"
}

resource "aws_eks_addon" "coredns" {
  cluster_name                = aws_eks_cluster.sandbox.name
  addon_name                  = "coredns"
  addon_version               = data.aws_eks_addon_version.coredns.version
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "PRESERVE"
}

resource "aws_eks_node_group" "sandbox" {
  cluster_name    = aws_eks_cluster.sandbox.name
  node_group_name = "${local.name_prefix}-workers"
  node_role_arn   = aws_iam_role.nodes.arn
  subnet_ids      = [aws_subnet.public[local.worker_az].id]
  instance_types  = ["t3.medium"]
  ami_type        = "AL2023_x86_64_STANDARD"
  capacity_type   = "ON_DEMAND"

  scaling_config {
    desired_size = 1
    min_size     = 1
    max_size     = 1
  }

  update_config {
    max_unavailable = 1
  }

  launch_template {
    id      = aws_launch_template.nodes.id
    version = aws_launch_template.nodes.latest_version
  }

  depends_on = [
    aws_eks_addon.vpc_cni,
    aws_eks_addon.kube_proxy,
    aws_eks_addon.coredns,
    aws_iam_role_policy_attachment.node_worker,
    aws_iam_role_policy_attachment.node_cni,
    aws_iam_role_policy_attachment.node_ecr,
    aws_vpc_endpoint.ecr_api,
    aws_vpc_endpoint.ecr_dkr,
    aws_vpc_endpoint.ec2,
    aws_vpc_endpoint.s3,
    aws_vpc_security_group_egress_rule.node_to_aws_endpoints,
    aws_vpc_security_group_egress_rule.node_to_s3,
    aws_vpc_security_group_egress_rule.node_to_control_plane,
    aws_vpc_security_group_ingress_rule.node_from_control_plane_kubelet,
    aws_vpc_security_group_ingress_rule.node_from_control_plane_https,
    aws_vpc_security_group_ingress_rule.endpoint_from_nodes,
  ]

  tags = { Name = "${local.name_prefix}-worker" }
}

resource "aws_iam_role" "nodes" {
  name = "${local.name_prefix}-nodes"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "node_worker" {
  role       = aws_iam_role.nodes.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonEKSWorkerNodePolicy"
}

resource "aws_iam_role_policy_attachment" "node_cni" {
  role       = aws_iam_role.nodes.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonEKS_CNI_Policy"
}

resource "aws_iam_role_policy_attachment" "node_ecr" {
  role       = aws_iam_role.nodes.name
  policy_arn = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AmazonEC2ContainerRegistryPullOnly"
}

resource "aws_launch_template" "nodes" {
  name_prefix            = "${local.name_prefix}-nodes-"
  update_default_version = true
  # Attach only the restricted worker group. EKS's cluster security group has
  # default broad egress; the explicit rules below permit required API flows.
  vpc_security_group_ids = [aws_security_group.nodes.id]

  credit_specification {
    cpu_credits = "standard"
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_size           = 20
      volume_type           = "gp3"
      encrypted             = true
      delete_on_termination = true
    }
  }

  tag_specifications {
    resource_type = "instance"
    tags          = { Name = "${local.name_prefix}-worker" }
  }

  tag_specifications {
    resource_type = "volume"
    tags          = { Name = "${local.name_prefix}-worker-root" }
  }
}

resource "aws_eks_access_entry" "operator" {
  cluster_name  = aws_eks_cluster.sandbox.name
  principal_arn = var.operator_iam_principal_arn
  type          = "STANDARD"
}

resource "aws_eks_access_policy_association" "operator_view" {
  cluster_name  = aws_eks_cluster.sandbox.name
  principal_arn = aws_eks_access_entry.operator.principal_arn
  policy_arn    = "arn:${data.aws_partition.current.partition}:eks::aws:cluster-access-policy/AmazonEKSViewPolicy"

  access_scope {
    type = "cluster"
  }
}

resource "aws_eks_access_policy_association" "operator_workloads" {
  cluster_name  = aws_eks_cluster.sandbox.name
  principal_arn = aws_eks_access_entry.operator.principal_arn
  policy_arn    = "arn:${data.aws_partition.current.partition}:eks::aws:cluster-access-policy/AmazonEKSAdminPolicy"

  access_scope {
    type       = "namespace"
    namespaces = [local.namespace]
  }
}
