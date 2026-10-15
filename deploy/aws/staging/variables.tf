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
variable "task_cpu" {
  type    = number
  default = 512
}
variable "task_memory" {
  type    = number
  default = 1024
}
variable "desired_count" {
  type    = number
  default = 0
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
