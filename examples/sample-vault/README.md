# Sample Vault

A small, fully sanitized operations vault that demonstrates the structure
`severino-vault-mcp` expects — and shows that a real vault is a *typed knowledge
base*, not a pile of loose notes. Everything here is fictional
(`*.internal.example` hosts, placeholder commands); no real infrastructure,
secrets, or hostnames appear.

Point the server at it and reproduce the whole demo flow:

```bash
SVMC_VAULT_PATH=examples/sample-vault uv run --no-editable severino-vault-mcp
```

The full transcript (broad question → Quick Index → target doc, plus the
sensitivity gate in action) lives in [`../../docs/demo.md`](../../docs/demo.md).
This vault is also a test fixture:
`tests/test_search.py::test_sample_vault_is_reproducible` asserts it stays
deterministic.

## Layout

```
01 Projects/        index notes that tie work together
02 Infrastructure/  system-of-record notes (one per system)
03 Runbooks/        the actual procedures + the Quick Index hub
.svmc/aliases.toml  query → doc_id aliases for fuzzy lookups
```

| Doc | `doc_id` | Shows |
|---|---|---|
| Quick Index | `report-playbook-mcp-index` | the navigation hub every broad question starts at |
| Client Edge DNS | `project-client-edge-dns` | a project index linking related docs |
| AdGuard Home | `infra-adguard-home` | an infrastructure system-of-record note |
| Offline CA | `infra-offline-ca` | a `restricted` doc — body withheld by the sensitivity gate |
| Add Nginx Proxy Host | `rb-add-nginx-proxy-host` | a runbook |
| Generate Internal Service Certificate | `rb-generate-internal-cert` | a runbook with `related_projects` |

## The data model

Every doc carries YAML frontmatter — this is the data model, not decoration. The
required fields are `doc_id`, `title`, `doc_type`, `system`, `environment`,
`status`, and `sensitivity`; docs also carry `last_reviewed`, `related_projects`,
`related_assets`, and `tags`. A stable `doc_id` (prefixed `rb-` / `infra-` /
`report-` / `project-` / `note-`) is the permanent address other docs and the
Quick Index link by, so titles can change without breaking references.

The field enums are owned by one source — the MCP's schema. Don't copy them;
emit them:

```bash
severino-vault-mcp schema --json
```

## What it demonstrates

- **Navigation by intent.** Broad questions start at the Quick Index, which
  routes intent → `doc_id` → the specific doc, instead of full-text guessing.
- **Typed relationships.** `related_projects` / `related_assets` plus the
  `doc_id` cross-links make the vault a graph, not a folder of files.
- **Policy in metadata, not prompts.** `Offline CA` is `restricted`: the MCP
  returns metadata and an advisory and withholds the body unless an explicit
  local unlock succeeds (see the demo transcript). The tier in frontmatter *is*
  the gate.
- **Aliases.** `.svmc/aliases.toml` maps natural phrases (`"cert generation"`)
  onto a `doc_id`, so fuzzy lookups still land on the right doc.

For the repo's own structure and the frontmatter contract in depth, see
[`../../STRUCTURE.md`](../../STRUCTURE.md) and
[`../../AGENTS.md`](../../AGENTS.md).
