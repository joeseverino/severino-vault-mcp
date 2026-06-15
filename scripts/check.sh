#!/usr/bin/env bash
# check.sh — the gate, runnable independently (locally, pre-push) and by CI (the
# reusable cordon gate runs it). It runs cordon's checks engine over this repo's
# cordon.checks.json — every check is declared as data there, so local and CI
# can't drift. This wrapper is identical in every cordon repo; what differs is
# only cordon.checks.json. Pass engine flags through, e.g. `scripts/check.sh --json`.
set -e
export CORDON_HOME="${CORDON_HOME:-${ASSETS_HOME:-$HOME/Documents/Code/Assets}/cordon}"
if [ ! -f "$CORDON_HOME/checks/run.mjs" ]; then
  echo "cordon not found (looked in: $CORDON_HOME)." >&2
  echo "One-time setup:  curl -fsSL https://raw.githubusercontent.com/joeseverino/cordon/main/install.sh | bash" >&2
  echo "Or point CORDON_HOME at an existing cordon checkout." >&2
  exit 1
fi
root="$(cd "$(dirname "$0")/.." && pwd)"
# The CI gate passes --ci; the engine detects CI on its own, so drop it.
[ "${1:-}" = "--ci" ] && shift
exec node "$CORDON_HOME/checks/run.mjs" --root "$root" "$@"
