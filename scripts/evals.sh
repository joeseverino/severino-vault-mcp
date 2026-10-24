#!/bin/sh
# Score the vault retrievers against the eval cases: the committed fixture by
# default, the real vault (read-only) when SVMC_EVAL_VAULT is set. Flags pass
# through: --json, --misses, --backends ours,grep, --cases <file>.
set -eu
cd "$(dirname "$0")/.."
exec go run ./internal/evals/cmd/evals "$@"
