locals {
  azs           = slice(var.availability_zones, 0, 2)
  public_hosts  = { for i, az in local.azs : az => i }
  private_hosts = { for i, az in local.azs : az => i + 10 }
}

resource "aws_vpc" "staging" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags                 = { Name = "${var.name_prefix}-vpc" }

}

resource "aws_internet_gateway" "staging" {
  vpc_id = aws_vpc.staging.id
  tags   = { Name = "${var.name_prefix}-igw" }
}

resource "aws_subnet" "public" {
  for_each                = local.public_hosts
  vpc_id                  = aws_vpc.staging.id
  availability_zone       = each.key
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, each.value)
  map_public_ip_on_launch = true
  tags                    = { Name = "${var.name_prefix}-public-${each.value}", "kubernetes.io/role/elb" = "1", "kubernetes.io/cluster/${var.name_prefix}" = "shared" }

}

resource "aws_subnet" "private" {
  for_each          = local.private_hosts
  vpc_id            = aws_vpc.staging.id
  availability_zone = local.azs[each.value - 10]
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, each.value)
  tags              = { Name = "${var.name_prefix}-private-${each.value - 10}", "kubernetes.io/role/internal-elb" = "1", "kubernetes.io/cluster/${var.name_prefix}" = "shared" }

}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.staging.id
  tags   = { Name = "${var.name_prefix}-public" }
}
resource "aws_route" "public_internet" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.staging.id
}
resource "aws_route_table_association" "public" {
  for_each       = aws_subnet.public
  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}

resource "aws_eip" "nat" {
  domain     = "vpc"
  depends_on = [aws_internet_gateway.staging]
  tags       = { Name = "${var.name_prefix}-nat" }
}
resource "aws_nat_gateway" "staging" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public[local.azs[0]].id
  depends_on    = [aws_internet_gateway.staging]
  tags          = { Name = "${var.name_prefix}-nat" }
}
resource "aws_route_table" "private" {
  for_each = local.private_hosts
  vpc_id   = aws_vpc.staging.id
  tags     = { Name = "${var.name_prefix}-private-${each.value - 10}" }
}
resource "aws_route" "private_nat" {
  for_each               = aws_route_table.private
  route_table_id         = each.value.id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.staging.id
}
resource "aws_route_table_association" "private" {
  for_each       = aws_subnet.private
  subnet_id      = each.value.id
  route_table_id = aws_route_table.private[each.key].id
}

resource "aws_security_group" "msk" {
  name   = "${var.name_prefix}-msk"
  vpc_id = aws_vpc.staging.id
  ingress {
    from_port       = 9096
    to_port         = 9096
    protocol        = "tcp"
    security_groups = [aws_security_group.eks_nodes.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

}
resource "aws_security_group" "db" {
  name   = "${var.name_prefix}-db"
  vpc_id = aws_vpc.staging.id
  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.eks_nodes.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

}

resource "aws_ecr_repository" "app" {
  name                 = "device-ingestion/staging"
  image_tag_mutability = "IMMUTABLE"
  image_scanning_configuration {
    scan_on_push = true
  }
}
resource "aws_ecr_lifecycle_policy" "app" {
  repository = aws_ecr_repository.app.name
  policy     = jsonencode({ rules = [{ rulePriority = 1, description = "Keep latest 20 images", selection = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = 20 }, action = { type = "expire" } }] })
}
resource "aws_db_subnet_group" "app" {
  name       = "${var.name_prefix}-db"
  subnet_ids = [for subnet in aws_subnet.private : subnet.id]
}
resource "aws_db_instance" "app" {
  identifier                  = "device-ingestion-staging"
  engine                      = "postgres"
  engine_version              = "17"
  instance_class              = var.db_instance_class
  allocated_storage           = 20
  max_allocated_storage       = 50
  db_name                     = "device_ingestion"
  username                    = "device_ingestion_admin"
  manage_master_user_password = true
  db_subnet_group_name        = aws_db_subnet_group.app.name
  vpc_security_group_ids      = [aws_security_group.db.id]
  publicly_accessible         = false
  storage_encrypted           = true
  backup_retention_period     = 3
  deletion_protection         = false
  skip_final_snapshot         = true
  auto_minor_version_upgrade  = true

}

resource "aws_msk_cluster" "app" {
  cluster_name           = "device-ingestion-staging"
  kafka_version          = "3.9.0"
  number_of_broker_nodes = 2
  broker_node_group_info {
    instance_type   = var.msk_instance_type
    client_subnets  = [for subnet in aws_subnet.private : subnet.id]
    security_groups = [aws_security_group.msk.id]
    storage_info {
      ebs_storage_info {
        volume_size = var.msk_ebs_gib
      }
    }
  }
  encryption_info {
    encryption_in_transit {
      client_broker = "TLS"
      in_cluster    = true
    }
  }
  client_authentication {
    sasl {
      scram = true
    }
  }
  logging_info {
    broker_logs {
      cloudwatch_logs {
        enabled   = true
        log_group = aws_cloudwatch_log_group.msk.name
      }
    }
  }
}
resource "aws_cloudwatch_log_group" "msk" {
  name              = "/aws/msk/device-ingestion/staging"
  retention_in_days = 14
}
resource "aws_msk_scram_secret_association" "app" {
  cluster_arn     = aws_msk_cluster.app.arn
  secret_arn_list = [var.msk_scram_secret_arn]
}
