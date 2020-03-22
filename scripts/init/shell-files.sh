#!/bin/bash

set -euo pipefail

if [[ ! -s .devcontainer/cache/bash_history ]]; then
    touch .devcontainer/cache/bash_history
fi

if [[ ! -s .devcontainer/cache/bashrc.local ]]; then
    cat <<'EOF' > .devcontainer/cache/bashrc.local
export PS1='`printf "%02X" $?`:\w `git branch 2> /dev/null | grep -E "^[*]" | sed -E "s/^\* +([^ ]+) *$/(\1) /"`\$ '
EOF

fi
