# Release Checklist

Normal flow:

```bash
scripts/prepare-release.sh X.Y.Z "short headline"
# edit CHANGELOG.md if needed
scripts/check.sh --release
git add ...
git commit
scripts/release.sh X.Y.Z "short headline"
```

The helper scripts are the source of truth. The sections below explain what
they cover and how to recover manually if needed.

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
scripts/check.sh
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

Use the prep helper for normal releases:

```bash
scripts/prepare-release.sh X.Y.Z "short headline"
```

It bumps `pyproject.toml`, `src/severino_vault_mcp/__init__.py`, README
status, creates a starter `CHANGELOG.md` section, and refreshes `uv.lock`.
Edit the generated changelog section before committing.

Manual fallback:

- `pyproject.toml` — the `version = "X.Y.Z"` line under `[project]`.
- `src/severino_vault_mcp/__init__.py` — the `__version__` string.
- `README.md` status paragraph.
- `CHANGELOG.md` release section and compare-link footer.
- `uv.lock` after `uv sync --extra dev`.

## CHANGELOG

- Add a new `## [X.Y.Z] - YYYY-MM-DD` section above the previous entry.
- Move anything currently under `[Unreleased]` into the new section.
- Update the compare-link footer at the bottom of the file:

  ```text
  [Unreleased]: https://github.com/joeseverino/severino-vault-mcp/compare/vX.Y.Z...HEAD
  [X.Y.Z]: https://github.com/joeseverino/severino-vault-mcp/compare/vPREV...vX.Y.Z
  ```

## Tag and Publish

Use the release helper for normal releases:

```bash
scripts/release.sh X.Y.Z "short headline"
```

It runs lint, tests, sample-vault validation, installed-tool smoke checks,
version alignment checks, creates an annotated tag, pushes `main` and the tag,
and creates the GitHub release from the matching `CHANGELOG.md` section.

Manual fallback, if needed:

```bash
git tag -a vX.Y.Z -m "vX.Y.Z - short headline"
git push origin main
git push origin vX.Y.Z
gh release create vX.Y.Z --title "vX.Y.Z - short headline" --latest
```

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
