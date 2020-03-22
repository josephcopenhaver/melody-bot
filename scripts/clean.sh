#!/bin/bash
set -eo pipefail

# clean build artifacts
rm -rf build

# if in a container, short circuit

if [ -n "${IN_DOCKER_CONTAINER}" ]; then
    exit 0
fi

# handle cleaning up docker-compose env

function join_by { local IFS="$1"; shift; echo "$*"; }

COMPOSE_PROJECT_NAME="josephcopenhaver-discord-bot"
COMPOSE_IGNORE_ORPHANS="false"

export COMPOSE_FILE="$(find "$PWD/docker" -maxdepth 2 -name docker-compose.yml | tr '\n' ':' | sed -E 's/:+$//')"

# remove any container attached to docker-compose networks
docker ps --filter "network=${NETWORK_PREFIX_INFRASTRUCTURE}infrastructure" --filter "network=${NETWORK_PREFIX_FRONTEND}frontend" --format '{{.ID}}' | \
    while read -r id; do
        docker stop "$id" || true
        docker rm "$id" || true
    done

# ensure everything is torn down
docker-compose down

rm -rf .docker-volumes
mkdir -p .docker-volumes
