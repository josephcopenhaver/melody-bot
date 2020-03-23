#!/bin/bash
set -eo pipefail

BUILD="${BUILD:-${ONLY_BUILD:-n}}"

source ./scripts/source/functions.sh
source ./scripts/source/vars.sh

COMPOSE_FILES+=("$PWD/docker/networks/docker-compose.yml")

BASE_COMPOSE_FILE="$(join_by : "${COMPOSE_FILES[@]}")"

export ENV="empty"
./scripts/init/secrets.sh

cat docker/layers | \
    while read -r layer; do
        echo "$layer"
        COMPOSE_FILE="$BASE_COMPOSE_FILE:$PWD/docker/$layer/docker-compose.yml" docker-compose build
    done
