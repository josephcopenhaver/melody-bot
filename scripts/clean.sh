#!/bin/bash
set -eo pipefail

# clean build artifacts
rm -rf build

# if in a container, short circuit

if [ -n "${IN_DOCKER_CONTAINER}" ]; then
    exit 0
fi

REMOVE_VOLUMES=y ./scripts/down.sh

rm -rf .docker-volumes
mkdir -p .docker-volumes
