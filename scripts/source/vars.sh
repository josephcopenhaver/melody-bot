#!/bin/bash

export NETWORK_PREFIX_INFRASTRUCTURE="${NETWORK_PREFIX_INFRASTRUCTURE:-josephcopenhaver--discord-bot--}"
export NETWORK_PREFIX_FRONTEND="${NETWORK_PREFIX_FRONTEND:-josephcopenhaver--discord-bot--}"
export COMPOSE_PROJECT_NAME="josephcopenhaver--discord-bot"
export COMPOSE_IGNORE_ORPHANS="false"

COMPOSE_FILES=()
