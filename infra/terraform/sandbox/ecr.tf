resource "aws_ecr_repository" "sandbox" {
  name                 = "device-ingestion/sandbox"
  image_tag_mutability = "IMMUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  encryption_configuration {
    encryption_type = "AES256"
  }

  tags = { Name = "${local.name_prefix}-images" }
}

resource "aws_ecr_lifecycle_policy" "sandbox" {
  repository = aws_ecr_repository.sandbox.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged sandbox images after seven days"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 7
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep the five most recent immutable sandbox image tags"
        selection = {
          tagStatus   = "tagged"
          countType   = "imageCountMoreThan"
          countNumber = 5
        }
        action = { type = "expire" }
      }
    ]
  })
}

resource "aws_iam_role" "github_ecr_publisher" {
  name                 = "${local.name_prefix}-github-ecr-publisher"
  max_session_duration = 3600

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRoleWithWebIdentity"
      Principal = { Federated = var.github_oidc_provider_arn }
      Condition = {
        StringEquals = {
          "${local.github_oidc_host}:aud" = "sts.amazonaws.com"
          "${local.github_oidc_host}:sub" = local.github_subject
        }
      }
    }]
  })

  tags = { Name = "${local.name_prefix}-github-ecr-publisher" }
}

resource "aws_iam_role_policy" "github_ecr_publisher" {
  name = "push-device-ingestion-sandbox-images"
  role = aws_iam_role.github_ecr_publisher.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["ecr:GetAuthorizationToken"]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:CompleteLayerUpload",
          "ecr:DescribeImages",
          "ecr:InitiateLayerUpload",
          "ecr:PutImage",
          "ecr:UploadLayerPart",
        ]
        Resource = aws_ecr_repository.sandbox.arn
      }
    ]
  })
}
