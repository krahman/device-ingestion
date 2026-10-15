#!/usr/bin/env python3
"""Render non-secret Helm values from the GitHub staging environment."""

import json
import os
import sys


def required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SystemExit(f"required environment variable {name} is empty")
    return value


def scalar(value: str) -> str:
    return json.dumps(value)


def main() -> None:
    values = {
        "namespace": required("K8S_NAMESPACE"),
        "awsRegion": required("AWS_REGION"),
        "image": {"repository": required("IMAGE_REPOSITORY"), "tag": required("IMAGE_TAG")},
        "serviceAccount": {
            "name": "device-ingestion",
            "roleArn": required("APPLICATION_ROLE_ARN"),
        },
        "database": {
            "host": required("DATABASE_HOST"),
            "port": "5432",
            "name": "device_ingestion",
            "secretArn": required("DATABASE_SECRET_ARN"),
        },
        "kafka": {
            "brokers": required("KAFKA_BROKERS"),
            "secretArn": required("MSK_SCRAM_SECRET_ARN"),
            "topicReplicationFactor": int(os.environ.get("KAFKA_TOPIC_REPLICATION_FACTOR", "2")),
            "workers": 2,
        },
        "secrets": {
            "ocppStationsArn": required("OCPP_STATIONS_SECRET_ARN"),
            "ocpiPartnersArn": required("OCPI_PARTNERS_SECRET_ARN"),
        },
        "ocpi": {
            "countryCode": required("CPO_COUNTRY_CODE"),
            "partyId": required("CPO_PARTY_ID"),
            "baseUrl": f"https://api.{required('BASE_DOMAIN')}",
        },
        "ingress": {
            "enabled": True,
            "className": "alb",
            "groupName": "device-ingestion-staging",
            "certificateArn": required("CERTIFICATE_ARN"),
            "baseDomain": required("BASE_DOMAIN"),
            "idleTimeoutSeconds": 600,
            "deregistrationDelaySeconds": 60,
        },
        "ocpp": {"enabled": os.environ.get("OCPP_ENABLED", "true").lower() == "true"},
        "replicaCount": 1,
        "terminationGracePeriodSeconds": 80,
    }

    def emit(mapping: dict[str, object], indent: int = 0) -> None:
        pad = " " * indent
        for key, value in mapping.items():
            if isinstance(value, dict):
                print(f"{pad}{key}:")
                emit(value, indent + 2)
            elif isinstance(value, bool):
                print(f"{pad}{key}: {'true' if value else 'false'}")
            elif isinstance(value, int):
                print(f"{pad}{key}: {value}")
            else:
                print(f"{pad}{key}: {scalar(str(value))}")

    emit(values)


if __name__ == "__main__":
    try:
        main()
    except ValueError as error:
        print(f"invalid numeric staging setting: {error}", file=sys.stderr)
        raise SystemExit(1) from error
