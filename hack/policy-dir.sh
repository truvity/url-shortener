#!/usr/bin/env bash
# The directory of the truvity/policy module this repository builds against
# (the version go.mod pins), from the module cache. The shared schemas
# (schemas/), the configuration fixtures (config/testdata) and the contracts
# (docs/contracts) are read from there rather than copied here, so the loaders
# and the charts are always held to the contracts of the version they declare.
set -euo pipefail
cd "$(dirname "$0")/.."
go mod download github.com/truvity/policy >/dev/null
exec go list -m -f '{{.Dir}}' github.com/truvity/policy
