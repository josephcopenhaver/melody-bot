#!/bin/bash

export NETWORK_PREFIX_INFRASTRUCTURE="${NETWORK_PREFIX_INFRASTRUCTURE:-josephcopenhaver--melody-bot--}"
export NETWORK_PREFIX_FRONTEND="${NETWORK_PREFIX_FRONTEND:-josephcopenhaver--melody-bot--}"
export COMPOSE_PROJECT_NAME="josephcopenhaver--melody-bot"
export COMPOSE_IGNORE_ORPHANS="false"

COMPOSE_FILES=()
