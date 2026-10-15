#!/usr/bin/env bash
set -euo pipefail

task_definition="${1:?task definition ARN required}"
network_configuration="awsvpcConfiguration={subnets=[${ECS_PRIVATE_SUBNET_IDS}],securityGroups=[${ECS_APP_SECURITY_GROUP}],assignPublicIp=DISABLED}"
task_arn="$(aws ecs run-task \
  --cluster "$ECS_CLUSTER" \
  --task-definition "$task_definition" \
  --launch-type FARGATE \
  --network-configuration "$network_configuration" \
  --query 'tasks[0].taskArn' \
  --output text)"

if [[ -z "$task_arn" || "$task_arn" == "None" ]]; then
  echo "ECS did not start task $task_definition" >&2
  exit 1
fi

aws ecs wait tasks-stopped --cluster "$ECS_CLUSTER" --tasks "$task_arn"
exit_code="$(aws ecs describe-tasks --cluster "$ECS_CLUSTER" --tasks "$task_arn" --query 'tasks[0].containers[0].exitCode' --output text)"
if [[ "$exit_code" != "0" ]]; then
  stop_reason="$(aws ecs describe-tasks --cluster "$ECS_CLUSTER" --tasks "$task_arn" --query 'tasks[0].stoppedReason' --output text)"
  echo "ECS task failed (exit code $exit_code): $stop_reason" >&2
  exit 1
fi
