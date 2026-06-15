#!/usr/bin/env bash
# check.sh — the gate, runnable independently (locally, pre-push) and by CI (the
# reusable cordon gate runs it). It runs cordon's checks engine over this repo's
# cordon.checks.json — every check is declared as data there, so local and CI
# can't drift. This wrapper is identical in every cordon repo; what differs is
# only cordon.checks.json.
set -e
home="${CORDON_HOME:-${ASSETS_HOME:-$HOME/Documents/Code/Assets}/cordon}"
root="$(cd "$(dirname "$0")/.." && pwd)"
# The CI gate passes --ci; the engine detects CI on its own, so drop it.
[ "${1:-}" = "--ci" ] && shift
exec node "$home/checks/run.mjs" --root "$root" "$@"
