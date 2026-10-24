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
scripts/check.sh
mise run golden
```

In an MCP client, verify:

- `vault://labs/quick-index` is visible.
- `vault://labs/doc/rb-generate-internal-cert` returns the sample certificate runbook.
- `find("generate internal certificate")` ranks
  `rb-generate-internal-cert` first.
- `vault://labs/doc/infra-offline-ca` withholds the body by default.
- `read_doc("infra-offline-ca", include_restricted=True)` still requires
  a Touch ID approval through 1Password.

## Packaging

```bash
go install ./cmd/severino-vault-mcp
severino-vault-mcp --help
severino-vault-mcp --fingerprint
```

After a release is cut, the `release` workflow builds the darwin and linux
(amd64, arm64) archives, uploads them with `SHA256SUMS`, and attests their
provenance. Confirm the release page lists them, then confirm MCP client examples in `README.md` and `QUICKSTART.md` still match
the installed command name.

## Renovate Pull Requests

Renovate (`renovate.json`) opens PRs for `gomod`, GitHub Actions, the tools in
`mise.toml` and the `cordon-spec` version in `scripts/check.sh`. It waits 7 days
after a release before proposing it, and keeps the Actions pinned to a SHA with
the version comment updated, e.g. `uses: actions/checkout@<sha> # v7.0.1`.
The `codeql-action` steps arrive as one PR so init and analyze stay on one
version.

To merge:

- Confirm the one required status check, `cordon / gate`, is green. The
  repository ruleset on `main` enforces it on every PR.
- If the PR modifies anything under `.github/workflows/`, your local `gh`
  token needs the `workflow` scope. One-time fix:

  ```bash
  gh auth refresh -s workflow
  ```

- The ruleset lets the repository admin bypass it, so a passing PR from the
  solo maintainer lands without a self-review round-trip:

  ```bash
  gh pr merge <num> --squash --delete-branch --admin
  ```

If a PR proposes a major version jump, skim the upstream release notes in the
PR body before merging. A green gate (gofmt, vet, test, govulncheck, golden
contracts) plus CodeQL is sufficient signal to merge.

## GitHub Release Notes

- Focus on user impact rather than commit churn.
- Link to `QUICKSTART.md`, `docs/ai-safety-security.md`, and
  `config.example.toml` when relevant.
- Note any new required environment variables, breaking config changes, or
  changes to the sensitivity gate.
