#!/bin/bash

ENV="${ENV:-empty}"

set -euo pipefail

if [ ! -f "secrets/$ENV.env" ]; then
    touch "secrets/$ENV.env"
fi
