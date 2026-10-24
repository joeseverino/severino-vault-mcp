# Release Checklist

A release is a `vX.Y.Z` tag pushed from `main`. Pick the version from the
Conventional Commit titles since the last tag (`fix` a patch, `feat` a minor,
`!` or a `BREAKING CHANGE` footer a major), then:

```bash
git tag v3.0.0
git push origin v3.0.0
```

The `release` workflow builds the darwin and linux (amd64, arm64) archives,
writes `SHA256SUMS`, attests their provenance, and creates the GitHub Release
with generated notes and those files attached. A major above v1 needs go.mod's
module path to end in `/vN` first, or `go install ...@latest` cannot resolve
it; the workflow refuses a tag whose major does not match.

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
mise run check
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

After the tag's `release` run, confirm the release page lists the four
archives and `SHA256SUMS`, that
`go install github.com/joeseverino/severino-vault-mcp/v3/cmd/severino-vault-mcp@latest`
installs the new version, and that the MCP client examples in `README.md` and
`QUICKSTART.md` still match the installed command name.

## Renovate Pull Requests

Renovate (`renovate.json`) opens PRs for `gomod`, GitHub Actions and the tools
in `mise.toml`. It waits 7 days after a release before proposing it, and keeps the Actions pinned to a SHA with
the version comment updated, e.g. `uses: actions/checkout@<sha> # v7.0.1`.
The `codeql-action` steps arrive as one PR so init and analyze stay on one
version.

To merge:

- Confirm the one required status check, `mise run check`, is green. The
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
PR body before merging. A green gate (gofmt, vet, test, govulncheck, golangci-lint,
golden contracts) plus CodeQL is sufficient signal to merge.

## GitHub Release Notes

The generated notes list the merged PRs since the last tag; they are the
changelog. For a minor or major release, edit them on the release page:

- Focus on user impact rather than commit churn.
- Link to `QUICKSTART.md`, `docs/ai-safety-security.md`, and
  `config.example.toml` when relevant.
- Note any new required environment variables, breaking config changes, or
  changes to the sensitivity gate.
