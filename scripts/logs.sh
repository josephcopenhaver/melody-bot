#!/bin/bash
set -eo pipefail

if [ "$IN_DOCKER_CONTAINER" == "y" ]; then
    echo "cannot run within a docker container"
    exit 1
fi

SERVICE_NAMES=( "$@" )

source ./scripts/source/functions.sh
source ./scripts/source/vars.sh

COMPOSE_FILES+=("$PWD/docker/networks/docker-compose.yml")
COMPOSE_FILES+=("$PWD/docker/default/docker-compose.yml")

export COMPOSE_FILE="$(join_by : "${COMPOSE_FILES[@]}")"

# make sure env file exists
export ENV="empty"
./scripts/init/secrets.sh

set -x

docker-compose logs -f "${SERVICE_NAMES[@]}"
