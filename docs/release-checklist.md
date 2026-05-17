# Release Checklist

Use this before tagging a public release or linking the project from a
portfolio.

## Repository Hygiene

- `README.md` explains the value proposition in the first screen.
- `QUICKSTART.md` gets a new user from clone to sample vault without private
  paths.
- `config.example.toml` contains only generic, safe defaults.
- `.github/SECURITY.md` has the current vulnerability-reporting email.
- No private vault paths, hostnames, tokens, or client data are present.
- The sample vault contains safe demo data only.

## Verification

```bash
uv sync --extra dev
uv run pytest
uv run ruff check .
SVMC_VAULT_PATH=examples/sample-vault uv run --no-editable severino-vault-mcp
```

In an MCP client, verify:

- `vault://quick-index` is visible.
- `vault://doc/rb-generate-internal-cert` returns the sample certificate runbook.
- `find_runbook("generate internal certificate")` ranks
  `rb-generate-internal-cert` first.
- `vault://doc/infra-offline-ca` withholds the body by default.
- `read_doc("infra-offline-ca", include_secret_adjacent=True)` still requires
  local unlock.

## Packaging

```bash
uv tool install --from . severino-vault-mcp --force
severino-vault-mcp --help
```

Then confirm MCP client examples in `README.md` and `QUICKSTART.md` still match
the installed command name.

## Version Bump

Version is duplicated in two files. Bump both, or the CI smoke test will
detect the drift:

- `pyproject.toml` — the `version = "X.Y.Z"` line under `[project]`.
- `src/severino_vault_mcp/__init__.py` — the `__version__` string.

Re-run `uv sync --extra dev` after the bump so `uv.lock` picks up the new
project version. Commit all three files together.

## CHANGELOG

- Add a new `## [X.Y.Z] — YYYY-MM-DD` section above the previous entry.
- Move anything currently under `[Unreleased]` into the new section.
- Update the compare-link footer at the bottom of the file:

  ```text
  [Unreleased]: https://github.com/joeseverino/severino-vault-mcp/compare/vX.Y.Z...HEAD
  [X.Y.Z]: https://github.com/joeseverino/severino-vault-mcp/compare/vPREV...vX.Y.Z
  ```

## Tag and Publish

Use annotated tags (`-a`), never lightweight tags. Tag format is
`vMAJOR.MINOR.PATCH`.

```bash
git tag -a vX.Y.Z -m "vX.Y.Z — short headline"
git push origin main
git push origin vX.Y.Z

gh release create vX.Y.Z \
  --title "vX.Y.Z — short headline" \
  --latest \
  --notes "$(awk '/^## \[X.Y.Z\]/,/^## \[/' CHANGELOG.md | sed '$d')"
```

The `awk | sed` pulls the X.Y.Z section out of `CHANGELOG.md` so the release
notes stay in lockstep with the changelog. Edit the section heading inside
the awk pattern to match the new version.

## Dependabot Pull Requests

Dependabot will open PRs for both `pip` and `github-actions` updates. They
arrive pre-pinned to the new SHA with the version comment updated, e.g.
`uses: actions/checkout@<sha> # v6.0.2`.

To merge:

- Confirm all five required status checks are green in the PR. The branch
  protection ruleset enforces this on every PR.
- If the PR modifies anything under `.github/workflows/`, your local `gh`
  token needs the `workflow` scope. One-time fix:

  ```bash
  gh auth refresh -s workflow
  ```

- Admin-merge is permitted because the branch protection rule is configured
  with `enforce_admins: false` specifically so the solo maintainer can land
  passing-CI PRs without a self-review round-trip:

  ```bash
  gh pr merge <num> --squash --delete-branch --admin
  ```

If a PR proposes a major version jump (for example `v3 → v8`), skim the
upstream release notes Dependabot includes in the PR body before merging.
CI is the final safety net — a green matrix across Python 3.11/3.12/3.13
plus CodeQL plus pip-audit is sufficient signal to merge.

## GitHub Release Notes

- Focus on user impact rather than commit churn.
- Link to `QUICKSTART.md`, `docs/ai-safety-security.md`, and
  `config.example.toml` when relevant.
- Note any new required environment variables, breaking config changes, or
  changes to the sensitivity gate.
