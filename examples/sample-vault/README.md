# Sample Vault

A small, fully sanitized operations vault that mirrors the structure of a real
`severino-vault-mcp` vault — and shows that a real vault is a *typed knowledge
base*, not a pile of loose notes. Everything here is fictional
(`*.internal.example` hosts, placeholder commands); no real infrastructure,
secrets, or hostnames appear.

Point the server at it and reproduce the whole demo flow:

```bash
SVMC_VAULT_PATH=examples/sample-vault go run ./cmd/severino-vault-mcp
```

The full transcript (broad question → Quick Index → target doc, plus the
sensitivity gate in action) lives in [`../../docs/demo.md`](../../docs/demo.md).
This exact vault is also wired into CI:
[`TestDoctorPassesOnTheSampleVault`](../../internal/doctor/doctor_test.go)
validates it, and
[`TestSampleVaultIsReproducible`](../../internal/core/core_test.go) asserts
its retrieval stays deterministic, so it can't silently drift from the
behavior documented here. See
[Sample Vault Reproducibility](../../docs/testing-ci.md#sample-vault-reproducibility)
in the testing/CI doc.

## Layout

The folders mirror a real vault's top-level shape:

```
00 Inbox/           unprocessed captures, promoted into structured docs   ── read-only
00 Templates/       the skeletons every operational doc is created from
01 Projects/        index notes that tie work together          ┐
02 Infrastructure/  system-of-record notes (one folder/system) │
03 Runbooks/        the procedures + the Quick Index hub        ├─ indexed + validated
04 Reference/       background primers                          ┘
05 Writeups/        public articles — the site's CMS (separate frontmatter) ┐ read-only
06 Pages/           static-site pages sourced from the vault                ┘
99 Archive/         superseded docs, kept for history
.svmc/aliases.toml  query → doc_id aliases for fuzzy lookups
```

`01 Projects` through `04 Reference` (and `07 Backlog`, when present) are indexed
and frontmatter-validated by default (`indexed_dirs`). `00 Inbox`, `05 Writeups`
and `06 Pages` are read-only (`read_only_dirs`): `find` and `read_doc` serve them,
while writes and `validate` leave their own frontmatter alone. A writeup or page
without a `doc_id` is known as `writeup-<slug>` or `page-<slug>`. The templates
(`00 Templates`) and the archive (`99 Archive`) are never indexed.

### Indexed operational docs

| Doc | `doc_id` | Shows |
|---|---|---|
| Quick Index | `report-playbook-mcp-index` | the navigation hub every broad question starts at |
| Navigate by a Quick Index | `report-quick-index-navigation` | a decision record (under `02 Infrastructure/00 Reporting/`) |
| Client Edge DNS | `project-client-edge-dns` | a project index linking related docs |
| AdGuard Home | `infra-adguard-home` | an infrastructure system-of-record note |
| Offline CA | `infra-offline-ca` | a `restricted` doc — body withheld by the sensitivity gate |
| Add Nginx Proxy Host | `rb-add-nginx-proxy-host` | a runbook |
| Generate Internal Service Certificate | `rb-generate-internal-cert` | a runbook with `related_projects` |

## The data model

Every operational doc carries YAML frontmatter — this is the data model, not
decoration. The required fields are `doc_id`, `title`, `doc_type`, `system`,
`environment`, `status`, and `sensitivity`; docs also carry `last_reviewed`,
`related_projects`, `related_assets`, and `tags`. A stable `doc_id` (prefixed
`rb-` / `infra-` / `report-` / `project-` / `note-`) is the permanent address
other docs and the Quick Index link by, so titles can change without breaking
references. Each doc is created from a skeleton in `00 Templates/`.

The field enums are owned by one source — the MCP's schema. Don't copy them;
emit them:

```bash
severino-vault-mcp schema --json
```

Writeups (`05 Writeups/<slug>/index.md`) and pages (`06 Pages/<page>/index.md`)
use a lighter, site-facing frontmatter instead, because they feed the publishing
pipeline rather than the runbook index.

## What it demonstrates

- **Navigation by intent.** Broad questions start at the Quick Index, which
  routes intent → `doc_id` → the specific doc, instead of full-text guessing.
- **Typed relationships.** `related_projects` / `related_assets` plus the
  `doc_id` cross-links make the vault a graph, not a folder of files.
- **Policy in metadata, not prompts.** `Offline CA` is `restricted`: the MCP
  returns metadata and an advisory and withholds the body unless an explicit
  local unlock succeeds (see the demo transcript). The tier in frontmatter *is*
  the gate.
- **A document lifecycle.** Capture in `00 Inbox`, promote from a template into
  the indexed dirs, retire to `99 Archive` (`status: archived`) when superseded.
- **The vault is also the CMS.** `05 Writeups` and `06 Pages` are the same
  markdown the public site publishes — content lives with operations, gated by
  `published`.
- **Aliases.** `.svmc/aliases.toml` maps natural phrases (`"cert generation"`)
  onto a `doc_id`, so fuzzy lookups still land on the right doc.

For the repo's own structure and the frontmatter contract in depth, see
[`../../STRUCTURE.md`](../../STRUCTURE.md) and
[`../../AGENTS.md`](../../AGENTS.md).
