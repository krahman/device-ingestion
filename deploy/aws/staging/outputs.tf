output "ecr_repository_url" {
  value = aws_ecr_repository.app.repository_url
}
output "aws_region" {
  value = var.aws_region
}
output "eks_cluster_name" {
  value = aws_eks_cluster.staging.name
}
output "vpc_id" {
  value = aws_vpc.staging.id
}
output "private_subnet_ids" {
  value = [for subnet in aws_subnet.private : subnet.id]
}
output "application_role_arn" {
  value = aws_iam_role.application.arn
}
output "application_namespace" {
  value = var.application_namespace
}
output "deployment_values" {
  value = {
    region                   = var.aws_region
    cluster_name             = aws_eks_cluster.staging.name
    namespace                = var.application_namespace
    image_repository         = aws_ecr_repository.app.repository_url
    msk_brokers              = aws_msk_cluster.app.bootstrap_brokers_sasl_scram
    db_host                  = aws_db_instance.app.address
    db_port                  = aws_db_instance.app.port
    db_name                  = aws_db_instance.app.db_name
    msk_scram_secret_arn     = var.msk_scram_secret_arn
    database_secret_arn      = aws_db_instance.app.master_user_secret[0].secret_arn
    ocpp_stations_secret_arn = var.ocpp_stations_secret_arn
    ocpi_partners_secret_arn = var.ocpi_partners_secret_arn
    certificate_arn          = var.certificate_arn
    base_domain              = var.base_domain
    cpo_country_code         = var.cpo_country_code
    cpo_party_id             = var.cpo_party_id
  }
}
output "github_deploy_role_arn" {
  value = aws_iam_role.github_actions.arn
}
