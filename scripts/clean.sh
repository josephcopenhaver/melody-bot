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

# remove any attached vscode dev-container
docker rm -f josephcopenhaver--discord-bot--shell || true

docker-compose down

rm -rf .docker-volumes
mkdir -p .docker-volumes
