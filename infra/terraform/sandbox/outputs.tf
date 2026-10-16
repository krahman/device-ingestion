output "aws_region" {
  description = "AWS region containing the isolated sandbox."
  value       = local.region
}

output "eks_cluster_name" {
  value = aws_eks_cluster.sandbox.name
}

output "eks_cluster_endpoint" {
  value = aws_eks_cluster.sandbox.endpoint
}

output "eks_cluster_version" {
  value = aws_eks_cluster.sandbox.version
}

output "eks_addon_versions" {
  description = "Exact add-on versions selected by the regional EKS API during planning."
  value = {
    vpc_cni    = aws_eks_addon.vpc_cni.addon_version
    kube_proxy = aws_eks_addon.kube_proxy.addon_version
    coredns    = aws_eks_addon.coredns.addon_version
  }
}

output "worker_instance_type" {
  value = "t3.medium"
}

output "ecr_repository_url" {
  value = aws_ecr_repository.sandbox.repository_url
}

output "github_ecr_publisher_role_arn" {
  value = aws_iam_role.github_ecr_publisher.arn
}

output "operator_kubeconfig_command" {
  value = "aws eks update-kubeconfig --region ${local.region} --name ${aws_eks_cluster.sandbox.name}"
}

output "resource_inventory" {
  description = "Terraform-managed sandbox inventory. Capture this output after apply and again after destroy."
  value = {
    region         = local.region
    cluster        = { type = "aws_eks_cluster", name = aws_eks_cluster.sandbox.name, arn = aws_eks_cluster.sandbox.arn }
    node_group     = { type = "aws_eks_node_group", name = aws_eks_node_group.sandbox.node_group_name, arn = aws_eks_node_group.sandbox.arn }
    vpc            = { type = "aws_vpc", id = aws_vpc.sandbox.id, cidr = aws_vpc.sandbox.cidr_block }
    subnets        = [for az, subnet in aws_subnet.public : { type = "aws_subnet", id = subnet.id, az = az, cidr = subnet.cidr_block }]
    endpoints      = [for endpoint in [aws_vpc_endpoint.ecr_api, aws_vpc_endpoint.ecr_dkr, aws_vpc_endpoint.ec2, aws_vpc_endpoint.s3] : { type = "aws_vpc_endpoint", id = endpoint.id, service = endpoint.service_name }]
    ecr_repository = { type = "aws_ecr_repository", name = aws_ecr_repository.sandbox.name, arn = aws_ecr_repository.sandbox.arn }
    iam_roles      = [for role in [aws_iam_role.cluster, aws_iam_role.nodes, aws_iam_role.github_ecr_publisher] : { type = "aws_iam_role", name = role.name, arn = role.arn }]
  }
}
