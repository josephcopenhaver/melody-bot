#!/bin/bash

if [ "$IN_DOCKER_CONTAINER" == "y" ]; then
    echo "cannot run within a docker container"
    exit 1
fi

set -eo pipefail

source ./scripts/source/functions.sh
source ./scripts/source/vars.sh

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
