#!/bin/bash
set -eo pipefail

if [ "$IN_DOCKER_CONTAINER" == "y" ]; then
    echo "cannot run within a docker container"
    exit 1
fi

# handle cleaning up docker-compose env

source ./scripts/source/functions.sh
source ./scripts/source/vars.sh

export COMPOSE_FILE="$(find "$PWD/docker" -maxdepth 2 -name docker-compose.yml | tr '\n' ':' | sed -E 's/:+$//')"

# remove any container attached to docker-compose networks (3 steps)

# 1. enumerate all containers attached to the networks
container_ids=()
while read -r id; do
    container_ids+=("$id")
done < <(docker ps -a --filter "network=${NETWORK_PREFIX_INFRASTRUCTURE}infrastructure" --filter "network=${NETWORK_PREFIX_FRONTEND}frontend" --format '{{.ID}}')

# 2. stop all containers attached to the networks
for id in "${container_ids[@]}"; do
    docker stop "$id" >/dev/null &
done
wait # no need to keep track of child pids

# 3. remove all containers attached to the networks
for id in "${container_ids[@]}"; do
    docker rm "$id" >/dev/null &
done
wait # no need to keep track of child pids

export ENV="empty"
./scripts/init/secrets.sh

COMPOSE_OPTS=()

if [ "$REMOVE_VOLUMES" == "y" ]; then
    COMPOSE_OPTS+=("-v")
fi

set -x

# ensure everything including networks are torn down
docker-compose down "${COMPOSE_OPTS[@]}"
