#!/bin/bash
set -eo pipefail

function join_by { local IFS="$1"; shift; echo "$*"; }

COMPOSE_PROJECT_NAME="josephcopenhaver-discord-bot"
COMPOSE_IGNORE_ORPHANS="false"

export COMPOSE_FILE="$(find "$PWD/docker" -maxdepth 2 -name docker-compose.yml | tr '\n' ':' | sed -E 's/:+$//')"

docker-compose down

rm -rf .docker-volumes
mkdir -p .docker-volumes
