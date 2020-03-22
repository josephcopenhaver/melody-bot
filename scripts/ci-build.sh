#!/bin/bash

set -euo pipefail

if [ ! -f go.sum ]; then
    touch go.sum
fi

before_sum_sig="$(shasum go.sum)"
before_mod_sig="$(shasum go.mod)"

./scripts/build

after_sum_sig="$(shasum go.sum)"
after_mod_sig="$(shasum go.mod)"

# cleanup the useless sum file if it is empty
if [ ! -s go.sum ]; then
    rm -f go.sum
fi

# verify no dependencies shifted
if [[ "${after_sum_sig}" != "${before_sum_sig}" ]] || [[ "${after_mod_sig}" != "${before_mod_sig}" ]]; then
    echo "looks like dependencies shifted, you need to run 'go mod vendor'"
    exit 1
fi
