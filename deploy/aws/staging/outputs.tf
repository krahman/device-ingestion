output "ecr_repository_url" {
  value = aws_ecr_repository.app.repository_url
}
output "ecs_cluster_name" {
  value = aws_ecs_cluster.app.name
}
output "ecs_service_name" {
  value = aws_ecs_service.app.name
}
output "app_task_definition_arn" {
  value = aws_ecs_task_definition.app.arn
}
output "migration_task_definition_arn" {
  value = aws_ecs_task_definition.migrate.arn
}
output "kafka_init_task_definition_arn" {
  value = aws_ecs_task_definition.kafka_init.arn
}
output "smoke_task_definition_arn" {
  value = aws_ecs_task_definition.smoke.arn
}
output "private_subnet_ids" {
  value = [for subnet in aws_subnet.private : subnet.id]
}
output "app_security_group_id" {
  value = aws_security_group.app.id
}
output "alb_dns_name" {
  value = aws_lb.app.dns_name
}
output "https_endpoints" {
  value = { http_ocpi = "https://api.${var.base_domain}", ocpp = "wss://ocpp.${var.base_domain}/ocpp", grpc = "grpcs://grpc.${var.base_domain}" }
}
output "github_deploy_role_arn" {
  value = aws_iam_role.github_actions.arn
}
