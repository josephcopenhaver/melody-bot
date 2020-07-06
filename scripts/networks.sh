#!/bin/bash

set -eo pipefail

if [ "$IN_DOCKER_CONTAINER" == "y" ]; then
    echo "cannot run within a docker container"
    exit 1
fi

BUILD="${BUILD:-${ONLY_BUILD:-n}}"

source ./scripts/source/functions.sh
source ./scripts/source/vars.sh

COMPOSE_FILES+=("$PWD/docker/networks/docker-compose.yml")
COMPOSE_FILES+=("$PWD/docker/shell/docker-compose.yml")

export COMPOSE_FILE="$(join_by : "${COMPOSE_FILES[@]}")"

./scripts/init/shell-files.sh

set -x

if [ "$BUILD" == "y" ]; then
    docker-compose build shell
    if [ "$ONLY_BUILD" == "y" ]; then
        exit 0
    fi
fi

docker-compose run --rm --entrypoint bash shell -c "exit 0"
