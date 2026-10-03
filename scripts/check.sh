#!/usr/bin/env bash
# The gate CI runs, run locally: cordon's checks engine over this repo.
set -euo pipefail
cd "$(dirname "$0")/.."
exec npx --yes --package cordon-spec@2 cordon-checks --root . "$@"
