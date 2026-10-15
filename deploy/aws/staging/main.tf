locals {
  azs                  = slice(var.availability_zones, 0, 2)
  public_hosts         = { for i, az in local.azs : az => i }
  private_hosts        = { for i, az in local.azs : az => i + 10 }
  aws_partition        = split(":", var.github_oidc_provider_arn)[1]
  aws_account          = split(":", var.github_oidc_provider_arn)[4]
  ecs_cluster_arn      = "arn:${local.aws_partition}:ecs:${var.aws_region}:${local.aws_account}:cluster/${aws_ecs_cluster.app.name}"
  ecs_service_arn      = "arn:${local.aws_partition}:ecs:${var.aws_region}:${local.aws_account}:service/${aws_ecs_cluster.app.name}/${aws_ecs_service.app.name}"
  task_families        = ["device-ingestion-staging", "device-ingestion-staging-kafka-init", "device-ingestion-staging-migrate", "device-ingestion-staging-smoke"]
  task_definition_arns = [for family in local.task_families : "arn:${local.aws_partition}:ecs:${var.aws_region}:${local.aws_account}:task-definition/${family}*"]
  image                = "${aws_ecr_repository.app.repository_url}:bootstrap"
  common_env = [
    { name = "HTTP_ADDRESS", value = ":8080" },
    { name = "OCPP_ADDRESS", value = ":8081" },
    { name = "GRPC_ADDRESS", value = ":50051" },
    { name = "PPROF_ADDRESS", value = "127.0.0.1:6060" },
    { name = "KAFKA_BROKERS", value = aws_msk_cluster.app.bootstrap_brokers_sasl_scram },
    { name = "KAFKA_TLS", value = "true" },
    { name = "KAFKA_SASL_MECHANISM", value = "SCRAM-SHA-512" },
    { name = "KAFKA_TOPIC_REPLICATION_FACTOR", value = tostring(var.kafka_topic_replication_factor) },
    { name = "KAFKA_DB_WORKERS", value = "2" },
    { name = "DB_MAX_OPEN_CONNS", value = "2" },
    { name = "DATABASE_HOST", value = aws_db_instance.app.address },
    { name = "DATABASE_PORT", value = tostring(aws_db_instance.app.port) },
    { name = "DATABASE_NAME", value = aws_db_instance.app.db_name },
    { name = "DATABASE_SSLMODE", value = "verify-full" },
    { name = "DATABASE_SSLROOTCERT", value = "/etc/ssl/cert.pem" },
    { name = "DATABASE_TLS_VERIFY", value = "true" },
    { name = "OCPI_CPO_COUNTRY_CODE", value = var.cpo_country_code },
    { name = "OCPI_CPO_PARTY_ID", value = var.cpo_party_id },
    { name = "OCPI_BASE_URL", value = "https://api.${var.base_domain}" },
    { name = "STAGING_API_URL", value = "https://api.${var.base_domain}" },
    { name = "STAGING_OCPP_URL", value = "wss://ocpp.${var.base_domain}/ocpp" },
    { name = "STAGING_GRPC_TARGET", value = "grpc.${var.base_domain}:443" },
  ]
  common_secrets = [
    { name = "KAFKA_SASL_USERNAME", valueFrom = "${var.msk_scram_secret_arn}:username::" },
    { name = "KAFKA_SASL_PASSWORD", valueFrom = "${var.msk_scram_secret_arn}:password::" },
    { name = "DATABASE_USER", valueFrom = "${aws_db_instance.app.master_user_secret[0].secret_arn}:username::" },
    { name = "DATABASE_PASSWORD", valueFrom = "${aws_db_instance.app.master_user_secret[0].secret_arn}:password::" },
    { name = "OCPP_STATIONS", valueFrom = var.ocpp_stations_secret_arn },
    { name = "OCPI_EMSP_PARTNERS", valueFrom = var.ocpi_partners_secret_arn },
  ]
  log_options = {
    "awslogs-group"         = aws_cloudwatch_log_group.app.name
    "awslogs-region"        = var.aws_region
    "awslogs-stream-prefix" = "ecs"

  }

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
  tags                    = { Name = "${var.name_prefix}-public-${each.value}" }

}

resource "aws_subnet" "private" {
  for_each          = local.private_hosts
  vpc_id            = aws_vpc.staging.id
  availability_zone = local.azs[each.value - 10]
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, each.value)
  tags              = { Name = "${var.name_prefix}-private-${each.value - 10}" }

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

resource "aws_security_group" "alb" {
  name   = "${var.name_prefix}-alb"
  vpc_id = aws_vpc.staging.id
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

}
resource "aws_security_group" "app" {
  name   = "${var.name_prefix}-app"
  vpc_id = aws_vpc.staging.id
  ingress {
    from_port       = 8080
    to_port         = 8081
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  ingress {
    from_port       = 50051
    to_port         = 50051
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

}
resource "aws_security_group" "msk" {
  name   = "${var.name_prefix}-msk"
  vpc_id = aws_vpc.staging.id
  ingress {
    from_port       = 9096
    to_port         = 9096
    protocol        = "tcp"
    security_groups = [aws_security_group.app.id]
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
    security_groups = [aws_security_group.app.id]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

}

resource "aws_lb" "app" {
  name                       = "device-ingestion-staging"
  internal                   = false
  load_balancer_type         = "application"
  security_groups            = [aws_security_group.alb.id]
  subnets                    = [for subnet in aws_subnet.public : subnet.id]
  idle_timeout               = 600
  enable_deletion_protection = false

}
resource "aws_lb_target_group" "http" {
  name                 = "di-stage-http"
  port                 = 8080
  protocol             = "HTTP"
  target_type          = "ip"
  vpc_id               = aws_vpc.staging.id
  deregistration_delay = var.deregistration_delay_seconds
  health_check {
    path    = "/health/ready"
    matcher = "200"
  }
}
resource "aws_lb_target_group" "ocpp" {
  name                 = "di-stage-ocpp"
  port                 = 8081
  protocol             = "HTTP"
  target_type          = "ip"
  vpc_id               = aws_vpc.staging.id
  deregistration_delay = var.deregistration_delay_seconds
  health_check {
    port    = "8080"
    path    = "/health/ready"
    matcher = "200"
  }
}
resource "aws_lb_target_group" "grpc" {
  name                 = "di-stage-grpc"
  port                 = 50051
  protocol             = "HTTP"
  protocol_version     = "GRPC"
  target_type          = "ip"
  vpc_id               = aws_vpc.staging.id
  deregistration_delay = var.deregistration_delay_seconds
  health_check {
    path    = "/grpc.health.v1.Health/Check"
    matcher = "0"
  }
}

resource "aws_lb_listener" "https" {
  load_balancer_arn = aws_lb.app.arn
  port              = 443
  protocol          = "HTTPS"
  certificate_arn   = var.certificate_arn
  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.http.arn
  }

}
resource "aws_lb_listener_rule" "ocpp" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 10
  condition {
    host_header {
      values = ["ocpp.${var.base_domain}"]
    }
  }
  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.ocpp.arn
  }
}
resource "aws_lb_listener_rule" "grpc" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 20
  condition {
    host_header {
      values = ["grpc.${var.base_domain}"]
    }
  }
  condition {
    http_header {
      http_header_name = "content-type"
      values           = ["application/grpc*"]
    }
  }
  action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.grpc.arn
  }
}
resource "aws_lb_listener_rule" "hide_metrics" {
  listener_arn = aws_lb_listener.https.arn
  priority     = 1
  condition {
    path_pattern {
      values = ["/metrics"]
    }
  }
  action {
    type = "fixed-response"
    fixed_response {
      content_type = "text/plain"
      message_body = "Not found"
      status_code  = "404"

    }

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
resource "aws_cloudwatch_log_group" "app" {
  name              = "/ecs/device-ingestion/staging"
  retention_in_days = 14
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

resource "aws_ecs_cluster" "app" {
  name = "device-ingestion-staging"
}
resource "aws_iam_role" "task_execution" {
  name               = "device-ingestion-staging-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}
data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}
resource "aws_iam_role_policy_attachment" "task_execution" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}
resource "aws_iam_role_policy" "task_secrets" {
  name = "read-device-ingestion-staging-secrets"
  role = aws_iam_role.task_execution.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [
    { Effect = "Allow", Action = ["secretsmanager:GetSecretValue"], Resource = [var.msk_scram_secret_arn, var.ocpp_stations_secret_arn, var.ocpi_partners_secret_arn, aws_db_instance.app.master_user_secret[0].secret_arn] },
    { Effect = "Allow", Action = ["kms:Decrypt"], Resource = var.msk_scram_kms_key_arn }
  ] })

}
resource "aws_iam_role" "task" {
  name               = "device-ingestion-staging-task"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

locals {
  app_definition        = [{ name = "device-ingestion", image = local.image, essential = true, entryPoint = ["/usr/local/bin/server"], portMappings = [{ containerPort = 8080, protocol = "tcp" }, { containerPort = 8081, protocol = "tcp" }, { containerPort = 50051, protocol = "tcp" }], environment = local.common_env, secrets = local.common_secrets, logConfiguration = { logDriver = "awslogs", options = local.log_options }, healthCheck = { command = ["CMD-SHELL", "wget -q -O /dev/null http://127.0.0.1:8080/health/live || exit 1"], interval = 30, timeout = 5, retries = 3, startPeriod = 30 } }]
  migrate_definition    = [{ name = "migrate", image = local.image, essential = true, entryPoint = ["/usr/local/bin/migrate"], environment = local.common_env, secrets = local.common_secrets, logConfiguration = { logDriver = "awslogs", options = local.log_options } }]
  kafka_init_definition = [{ name = "kafka-init", image = local.image, essential = true, entryPoint = ["/usr/local/bin/kafka-init"], environment = local.common_env, secrets = local.common_secrets, logConfiguration = { logDriver = "awslogs", options = local.log_options } }]
  smoke_definition      = [{ name = "staging-smoke", image = local.image, essential = true, entryPoint = ["/usr/local/bin/staging-smoke"], environment = local.common_env, secrets = local.common_secrets, logConfiguration = { logDriver = "awslogs", options = local.log_options } }]

}
resource "aws_ecs_task_definition" "app" {
  family                   = "device-ingestion-staging"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.task.arn
  container_definitions    = jsonencode(local.app_definition)
}
resource "aws_ecs_task_definition" "migrate" {
  family                   = "device-ingestion-staging-migrate"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.task.arn
  container_definitions    = jsonencode(local.migrate_definition)
}
resource "aws_ecs_task_definition" "kafka_init" {
  family                   = "device-ingestion-staging-kafka-init"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.task.arn
  container_definitions    = jsonencode(local.kafka_init_definition)
}
resource "aws_ecs_task_definition" "smoke" {
  family                   = "device-ingestion-staging-smoke"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = aws_iam_role.task_execution.arn
  task_role_arn            = aws_iam_role.task.arn
  container_definitions    = jsonencode(local.smoke_definition)
}
resource "aws_ecs_service" "app" {
  name                               = "device-ingestion-staging"
  cluster                            = aws_ecs_cluster.app.id
  task_definition                    = aws_ecs_task_definition.app.arn
  desired_count                      = var.desired_count
  launch_type                        = "FARGATE"
  health_check_grace_period_seconds  = 180
  deployment_minimum_healthy_percent = 0
  deployment_maximum_percent         = 200
  enable_execute_command             = false
  network_configuration {
    subnets          = [for subnet in aws_subnet.private : subnet.id]
    security_groups  = [aws_security_group.app.id]
    assign_public_ip = false
  }
  load_balancer {
    target_group_arn = aws_lb_target_group.http.arn
    container_name   = "device-ingestion"
    container_port   = 8080
  }
  load_balancer {
    target_group_arn = aws_lb_target_group.ocpp.arn
    container_name   = "device-ingestion"
    container_port   = 8081
  }
  load_balancer {
    target_group_arn = aws_lb_target_group.grpc.arn
    container_name   = "device-ingestion"
    container_port   = 50051
  }
  depends_on = [aws_lb_listener.https, aws_msk_scram_secret_association.app]
  lifecycle {
    ignore_changes = [task_definition, desired_count]
  }

}

resource "aws_iam_role" "github_actions" {
  name               = "device-ingestion-staging-github-deploy"
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Principal = { Federated = var.github_oidc_provider_arn }, Action = "sts:AssumeRoleWithWebIdentity", Condition = { StringEquals = { "token.actions.githubusercontent.com:aud" = var.github_oidc_audience, "token.actions.githubusercontent.com:sub" = "repo:${var.github_repository}:environment:${var.github_environment}" } } }] })

}
resource "aws_iam_role_policy" "github_actions" {
  name = "deploy-device-ingestion-staging"
  role = aws_iam_role.github_actions.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["ecr:GetAuthorizationToken"]
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = ["ecr:BatchCheckLayerAvailability", "ecr:CompleteLayerUpload", "ecr:InitiateLayerUpload", "ecr:PutImage", "ecr:UploadLayerPart", "ecr:BatchGetImage", "ecr:DescribeImages", "ecr:GetDownloadUrlForLayer"]
        Resource = aws_ecr_repository.app.arn
      },
      {
        Effect   = "Allow"
        Action   = ["ecs:DescribeServices", "ecs:UpdateService"]
        Resource = local.ecs_service_arn
        Condition = {
          ArnEquals = { "ecs:cluster" = local.ecs_cluster_arn }
        }
      },
      {
        Effect   = "Allow"
        Action   = ["ecs:DescribeTaskDefinition"]
        Resource = "*"
      },
      {
        Effect   = "Allow"
        Action   = ["ecs:RegisterTaskDefinition"]
        Resource = local.task_definition_arns
      },
      {
        Effect   = "Allow"
        Action   = ["ecs:DescribeTasks", "ecs:ListTasks", "ecs:StopTask"]
        Resource = "*"
        Condition = {
          ArnEquals = { "ecs:cluster" = local.ecs_cluster_arn }
        }
      },
      {
        Effect   = "Allow"
        Action   = ["ecs:RunTask"]
        Resource = local.task_definition_arns
        Condition = {
          ArnEquals = { "ecs:cluster" = local.ecs_cluster_arn }
        }
      },
      {
        Effect   = "Allow"
        Action   = ["iam:PassRole"]
        Resource = [aws_iam_role.task_execution.arn, aws_iam_role.task.arn]
        Condition = {
          StringEquals = { "iam:PassedToService" = "ecs-tasks.amazonaws.com" }
        }
      }
    ]
  })

}
