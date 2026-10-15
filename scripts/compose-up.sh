#!/bin/sh
set -eu

# Populate local OCPP/OCPI examples on the first run. Keep developer overrides.
if [ ! -f .env ]; then
	cp .env.example .env
	printf 'Created .env from .env.example; edit it to configure local station and partner credentials.\n'
fi

# Stop legacy containers created by the former `make kafka-up` and
# `make postgres-up` targets so they release the ports used by Compose.
for container in device-ingestion-kafka device-ingestion-postgres; do
	if docker container inspect "$container" >/dev/null 2>&1; then
		running=$(docker container inspect --format '{{.State.Running}}' "$container")
		if [ "$running" = "true" ]; then
			docker stop "$container"
		fi
	fi
done

exec docker compose up --build "$@"
