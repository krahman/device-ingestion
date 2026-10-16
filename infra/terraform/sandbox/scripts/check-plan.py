#!/usr/bin/env python3
"""Reject Terraform plans that include resources excluded from the sandbox."""

import json
import sys


FORBIDDEN_PREFIXES = (
    "aws_db_",
    "aws_rds_",
    "aws_msk_",
    "aws_nat_",
    "aws_lb",
    "aws_elb",
    "aws_waf",
)
FORBIDDEN_EXACT = {"aws_vpc_nat_gateway"}


def collect_resources(module, found):
    for resource in module.get("resources", []):
        if resource.get("mode", "managed") == "managed":
            found.add(resource.get("type", ""))
    for child in module.get("child_modules", []):
        collect_resources(child, found)


def is_forbidden(resource_type):
    return resource_type in FORBIDDEN_EXACT or resource_type.startswith(FORBIDDEN_PREFIXES)


def main():
    try:
        plan = json.load(sys.stdin)
    except json.JSONDecodeError as error:
        print(f"Invalid Terraform plan JSON: {error}", file=sys.stderr)
        return 2

    resource_types = set()
    for change in plan.get("resource_changes", []):
        if change.get("mode", "managed") == "managed":
            resource_types.add(change.get("type", ""))

    planned_values = plan.get("planned_values") or {}
    root_module = planned_values.get("root_module")
    if root_module:
        collect_resources(root_module, resource_types)

    excluded = sorted(resource_type for resource_type in resource_types if is_forbidden(resource_type))
    if excluded:
        print("Sandbox plan guard failed; excluded resources found:", file=sys.stderr)
        for resource_type in excluded:
            print(f"  - {resource_type}", file=sys.stderr)
        return 1

    print(f"Sandbox plan guard passed ({len(resource_types)} managed resource types reviewed).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
