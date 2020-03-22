#!/bin/bash

if [ -n "$IN_DOCKER_CONTAINER" ]; then
    echo "cannot run within a docker container"
    exit 1
fi

set -eo pipefail

function join_by { local IFS="$1"; shift; echo "$*"; }

COMPOSE_PROJECT_NAME="josephcopenhaver-discord-bot"
COMPOSE_IGNORE_ORPHANS="false"
COMPOSE_FILES=()

COMPOSE_FILES+=("$PWD/docker/networks/docker-compose.yml")
COMPOSE_FILES+=("$PWD/docker/default/docker-compose.yml")

export COMPOSE_FILE="$(join_by : "${COMPOSE_FILES[@]}")"

# make sure env file exists
export ENV="${ENV:-test}"
./scripts/init/secrets.sh

set -x

if [ "$BUILD" == "y" ]; then
    docker-compose build
    if [ "$ONLY_BUILD" == "y" ]; then
        exit 0
    fi
fi

docker-compose up -d
