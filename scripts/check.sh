#!/usr/bin/env bash
# The gate CI runs, run locally: cordon's checks engine over this repo.
set -euo pipefail
cd "$(dirname "$0")/.."
# Agent and non-login shells may lack mise's shims; append them so go and npx
# resolve without shadowing tools already on PATH.
PATH="$PATH:$HOME/.local/share/mise/shims"
exec npx --yes --package cordon-spec@2 cordon-checks --root . "$@"
