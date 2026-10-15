variable "aws_region" {
  type = string
}
variable "availability_zones" {
  type        = list(string)
  description = "Two availability zones in aws_region for the staging subnets."
  validation {
    condition     = length(var.availability_zones) >= 2
    error_message = "At least two availability zones must be configured."
  }
}
variable "name_prefix" {
  type    = string
  default = "device-ingestion-staging"
}
variable "vpc_cidr" {
  type    = string
  default = "10.40.0.0/16"
}
variable "certificate_arn" {
  type        = string
  description = "ACM certificate covering api, ocpp, and grpc staging hostnames."
}
variable "base_domain" {
  type        = string
  description = "DNS suffix for api, ocpp, and grpc staging endpoints."
}
variable "github_repository" {
  type        = string
  description = "GitHub owner/repository, for example owner/device-ingestion."
}
variable "github_oidc_provider_arn" {
  type        = string
  description = "Existing AWS IAM OIDC provider ARN for token.actions.githubusercontent.com."
}
variable "github_oidc_audience" {
  type    = string
  default = "sts.amazonaws.com"
}
variable "github_environment" {
  type    = string
  default = "staging"
}
variable "kubernetes_version" {
  type        = string
  description = "Amazon EKS Kubernetes minor version available in the selected region."
}
variable "cluster_endpoint_public_access_cidrs" {
  type        = list(string)
  description = "CIDR blocks allowed to reach the EKS public API endpoint. Restrict this to operator or runner egress ranges when known."
  default     = ["0.0.0.0/0"]
}
variable "node_instance_types" {
  type        = list(string)
  description = "EC2 instance types for the private EKS managed node group."
  default     = ["t3.medium"]
}
variable "node_desired_size" {
  type        = number
  description = "Desired number of EKS worker nodes for the pilot."
  default     = 2
}
variable "node_min_size" {
  type    = number
  default = 1
}
variable "node_max_size" {
  type    = number
  default = 2
}
variable "application_namespace" {
  type    = string
  default = "device-ingestion-staging"
}
variable "msk_scram_secret_arn" {
  type        = string
  description = "Pre-created Secrets Manager secret ARN with JSON username and password fields."
}
variable "msk_scram_kms_key_arn" {
  type        = string
  description = "Customer-managed KMS key ARN used to encrypt the MSK SCRAM secret."
}
variable "ocpp_stations_secret_arn" {
  type        = string
  description = "Secrets Manager secret ARN containing OCPP_STATIONS JSON."
}
variable "ocpi_partners_secret_arn" {
  type        = string
  description = "Secrets Manager secret ARN containing OCPI_EMSP_PARTNERS JSON."
}
variable "cpo_country_code" {
  type    = string
  default = ""
}
variable "cpo_party_id" {
  type    = string
  default = ""
}
variable "deregistration_delay_seconds" {
  type        = number
  description = "Seconds that the ALB allows in-flight requests and WebSocket sessions to drain."
  default     = 60
  validation {
    condition     = var.deregistration_delay_seconds >= 30 && var.deregistration_delay_seconds <= 3600
    error_message = "deregistration_delay_seconds must be between 30 and 3600."
  }
}
variable "db_instance_class" {
  type    = string
  default = "db.t4g.micro"
}
variable "msk_instance_type" {
  type    = string
  default = "kafka.t3.small"
}
variable "msk_ebs_gib" {
  type    = number
  default = 20
}
variable "kafka_topic_replication_factor" {
  type    = number
  default = 2
}
