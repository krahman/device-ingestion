locals {
  name_prefix        = "device-ingestion-sandbox"
  region             = "ap-southeast-1"
  namespace          = "device-ingestion-sandbox"
  github_environment = "sandbox"

  availability_zones = slice(data.aws_availability_zones.available.names, 0, 2)
  public_subnet_specs = {
    for index, az in local.availability_zones : az => {
      index = index
      cidr  = cidrsubnet(var.vpc_cidr, 8, index)
    }
  }
  worker_az = local.availability_zones[0]

  github_oidc_host = trimprefix(data.aws_iam_openid_connect_provider.github.url, "https://")
  github_subject   = "repo:${var.github_repository}:environment:${local.github_environment}"
}

data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

data "aws_iam_openid_connect_provider" "github" {
  arn = var.github_oidc_provider_arn
}

data "aws_prefix_list" "s3" {
  name = "com.amazonaws.${local.region}.s3"
}
