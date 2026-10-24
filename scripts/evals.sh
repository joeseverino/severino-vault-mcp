#!/bin/sh
# Score the vault retrievers against the eval cases: the committed fixture by
# default, the real vault (read-only) when SVMC_EVAL_VAULT is set. Flags pass
# through: --json, --misses, --backends ours,grep, --cases <file>.
set -eu
cd "$(dirname "$0")/.."
# Agent and non-login shells may lack mise's shims; append them so go and npx
# resolve without shadowing tools already on PATH.
PATH="$PATH:$HOME/.local/share/mise/shims"
exec go run ./internal/evals/cmd/evals "$@"
