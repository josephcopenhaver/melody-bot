#!/bin/bash

set -euo pipefail

coverage_profile=build/testreports/coverage.profile

rm -f "$coverage_profile"

mkdir -p "$(dirname "$coverage_profile")"

set -x

go test -cover -p 1 -coverprofile "$coverage_profile" ./...
