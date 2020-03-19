#!/bin/bash
set -eo pipefail

BUILD="${BUILD:-${ONLY_BUILD:-n}}"

function join_by { local IFS="$1"; shift; echo "$*"; }

export NETWORK_PREFIX_INFRASTRUCTURE=""
export NETWORK_PREFIX_FRONTEND=""
COMPOSE_PROJECT_NAME="josephcopenhaver-discord-bot"
COMPOSE_IGNORE_ORPHANS="false"
COMPOSE_FILES=()

COMPOSE_FILES+=("$PWD/docker/networks/docker-compose.yml")

BASE_COMPOSE_FILE="$(join_by : "${COMPOSE_FILES[@]}")"
cat docker/layers | \
    while read -r layer; do
        echo "$layer"
        COMPOSE_FILE="$BASE_COMPOSE_FILE:$PWD/docker/$layer/docker-compose.yml" docker-compose build
    done
