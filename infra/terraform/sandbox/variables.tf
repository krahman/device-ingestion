variable "kubernetes_version" {
  description = "EKS Kubernetes minor version; confirm it is offered in ap-southeast-1 before planning."
  type        = string
  default     = "1.36"

  validation {
    condition     = can(regex("^1\\.[0-9]+$", var.kubernetes_version))
    error_message = "Use an EKS Kubernetes minor version such as 1.36."
  }
}

variable "vpc_cidr" {
  description = "Dedicated sandbox VPC IPv4 CIDR. Confirm it does not overlap any connected network."
  type        = string
  default     = "10.80.0.0/16"

  validation {
    condition     = can(cidrhost(var.vpc_cidr, 0))
    error_message = "vpc_cidr must be a valid IPv4 CIDR block."
  }
}

variable "allowed_operator_cidrs" {
  description = "Public IPv4 /32 CIDRs from which the operator workstation may reach the EKS public API endpoint."
  type        = list(string)

  validation {
    condition = length(var.allowed_operator_cidrs) > 0 && alltrue([
      for cidr in var.allowed_operator_cidrs : can(cidrnetmask(cidr)) && endswith(cidr, "/32")
    ])
    error_message = "Provide one or more valid IPv4 /32 CIDRs. Broad ranges are not accepted."
  }
}

variable "operator_iam_principal_arn" {
  description = "IAM user or IAM role ARN used by the learner's workstation to access EKS. Do not provide an STS assumed-role session ARN."
  type        = string

  validation {
    condition     = can(regex("^arn:aws:iam::[0-9]{12}:(role|user)/.+$", var.operator_iam_principal_arn))
    error_message = "Provide an IAM user or role ARN, not an STS session ARN."
  }
}

variable "github_repository" {
  description = "Exact GitHub owner/repository name used by the protected sandbox environment."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", var.github_repository))
    error_message = "Use the exact GitHub owner/repository form, such as owner/device-ingestion."
  }
}

variable "github_oidc_provider_arn" {
  description = "Existing account-level GitHub Actions OIDC provider ARN for token.actions.githubusercontent.com."
  type        = string

  validation {
    condition     = can(regex("^arn:aws:iam::[0-9]{12}:oidc-provider/token\\.actions\\.githubusercontent\\.com$", var.github_oidc_provider_arn))
    error_message = "Provide the existing IAM OIDC provider ARN for token.actions.githubusercontent.com."
  }
}
