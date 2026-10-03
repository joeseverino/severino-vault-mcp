# Release Checklist

Releases are cut by release-please through cordon's reusable release workflow.
Every push to `main` updates one standing release PR that bumps the version and
writes `CHANGELOG.md` from the Conventional Commit titles; merging it tags
`vX.Y.Z` and creates the GitHub Release. There is no manual version bump.

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

- `vault://labs/quick-index` is visible.
- `vault://labs/doc/rb-generate-internal-cert` returns the sample certificate runbook.
- `find("generate internal certificate")` ranks
  `rb-generate-internal-cert` first.
- `vault://labs/doc/infra-offline-ca` withholds the body by default.
- `read_doc("infra-offline-ca", include_restricted=True)` still requires
  local unlock.

## Packaging

```bash
uv tool install --from . severino-vault-mcp --force
severino-vault-mcp --help
```

Then confirm MCP client examples in `README.md` and `QUICKSTART.md` still match
the installed command name.

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
