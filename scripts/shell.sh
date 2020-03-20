#!/bin/bash
set -eo pipefail

BUILD="${BUILD:-${ONLY_BUILD:-n}}"

function join_by { local IFS="$1"; shift; echo "$*"; }

COMPOSE_PROJECT_NAME="josephcopenhaver-discord-bot"
COMPOSE_IGNORE_ORPHANS="false"
COMPOSE_FILES=()

COMPOSE_FILES+=("$PWD/docker/networks/docker-compose.yml")
COMPOSE_FILES+=("$PWD/docker/shell/docker-compose.yml")

export COMPOSE_FILE="$(join_by : "${COMPOSE_FILES[@]}")"

if [[ ! -s .devcontainer/cache/bash_history ]]; then
    touch .devcontainer/cache/bash_history
fi

if [[ ! -s .devcontainer/cache/bashrc.local ]]; then
    cat <<'EOF' > .devcontainer/cache/bashrc.local
export PS1='`printf "%02X" $?`:\w `git branch 2> /dev/null | grep -E "^[*]" | sed -E "s/^\* +([^ ]+) *$/(\1) /"`\$ '
EOF

fi

set -x

if [ "$BUILD" == "y" ]; then
    docker-compose build shell
    if [ "$ONLY_BUILD" == "y" ]; then
        exit 0
    fi
fi

docker-compose run --rm shell
