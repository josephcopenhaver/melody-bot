#!/bin/bash
set -eo pipefail

NETWORK="${NETWORK:-infrastructure}"

source ./scripts/source/functions.sh
source ./scripts/source/vars.sh

if [ "$NETWORK" == "infrastructure" ]; then
    NETWORK="${NETWORK_PREFIX_INFRASTRUCTURE}infrastructure"
elif [ "$NETWORK" == "frontend" ]; then
    NETWORK="${NETWORK_PREFIX_FRONTEND}frontend"
fi

COMPOSE_FILES+=("$PWD/docker/networks/docker-compose.yml")
COMPOSE_FILES+=("$PWD/docker/shell/docker-compose.yml")

export COMPOSE_FILE="$(join_by : "${COMPOSE_FILES[@]}")"

./scripts/networks.sh

mkdir -p "$HOME/.aws/cli/cache"

set -x

if [ "${ONLY_BUILD:-n}" == "y" ]; then
    exit 0
fi

docker run \
    \
    --rm -it \
    \
    "--network=${NETWORK}" \
    \
    --env-file "$PWD/.devcontainer/env" \
    \
    -e AWS_ACCESS_KEY_ID \
    -e AWS_SECRET_ACCESS_KEY \
    -e AWS_DEFAULT_REGION \
    -e AWS_REGION \
    -e AWS_DEFAULT_OUTPUT \
    -e AWS_PROFILE \
    -e AWS_SDK_LOAD_CONFIG \
    -e IN_DOCKER_CONTAINER=true \
    \
    -w "/workspace" \
    \
    -v "$PWD:/workspace" \
    -v "$HOME/.aws:/root/.aws:ro" \
    -v "$HOME/.aws/cli/cache:/root/.aws/cli/cache:rw" \
    -v "$HOME/.ssh:/root/.ssh:ro" \
    -v "$PWD/.devcontainer/cache/go:/go" \
    -v "$PWD/.devcontainer/cache/bashrc.local:/root/.bashrc.local" \
    -v "$PWD/.devcontainer/cache/bash_history:/root/.bash_history" \
    \
    --entrypoint bash \
    \
    "josephcopenhaver/discord-bot--shell:${GIT_SHA:-latest}"
