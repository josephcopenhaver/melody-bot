#!/bin/bash

ENV="${ENV:-empty}"

set -euo pipefail

mkdir -p secrets
if [ ! -f "secrets/$ENV.env" ]; then
    touch "secrets/$ENV.env"
fi
