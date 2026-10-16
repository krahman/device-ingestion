#!/usr/bin/env python3
"""Print a non-secret inventory of Terraform-managed sandbox resources."""

import json
import sys


def walk_module(module, records):
    for resource in module.get("resources", []):
        if resource.get("mode", "managed") != "managed":
            continue
        values = resource.get("values") or {}
        tags = values.get("tags_all") or values.get("tags") or {}
        region = values.get("region")
        if not region:
            region = "global" if (resource.get("type") or "").startswith("aws_iam_") else "ap-southeast-1"
        records.append(
            {
                "address": resource.get("address"),
                "type": resource.get("type"),
                "name": values.get("name") or tags.get("Name") or values.get("id"),
                "id": values.get("id"),
                "arn": values.get("arn"),
                "region": region,
                "tags": tags,
            }
        )
    for child in module.get("child_modules", []):
        walk_module(child, records)


def main():
    try:
        state = json.load(sys.stdin)
    except json.JSONDecodeError as error:
        print(f"Invalid Terraform state JSON: {error}", file=sys.stderr)
        return 2

    root_module = (state.get("values") or {}).get("root_module")
    if root_module is None:
        print("Terraform state JSON has no root module.", file=sys.stderr)
        return 2

    records = []
    walk_module(root_module, records)
    print(json.dumps(sorted(records, key=lambda record: record["address"] or ""), indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
