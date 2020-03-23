#!/bin/bash
set -eo pipefail

# clean build artifacts
rm -rf build

# if in a container, short circuit

if [ "$IN_DOCKER_CONTAINER" == "y" ]; then
    exit 0
fi

REMOVE_VOLUMES=y ./scripts/down.sh

set -x

rm -rf .docker-volumes
mkdir -p .docker-volumes
