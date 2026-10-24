# Migrating A Messy Obsidian Vault

`severino-vault-mcp` works best when operational docs have consistent
frontmatter. A real vault rarely starts that way. Use this guide to adopt the
tool incrementally instead of trying to clean everything at once.

## 1. Pick Indexed Folders

Start with a small operational subset:

```text
01 Projects/
02 Infrastructure/
03 Runbooks/
```

Point the MCP at only those folders:

```toml
[vault]
path = "/absolute/path/to/your/vault"
indexed_dirs = ["01 Projects", "02 Infrastructure", "03 Runbooks"]
```

Do not index daily notes, clipped articles, or general reference folders until
you know they add value.

## 2. Run Doctor

```bash
SVMC_VAULT_PATH="/absolute/path/to/your/vault" severino-vault-mcp doctor
```

This reports markdown files in indexed folders that are missing required
frontmatter fields or contain invalid enum values.

Use proposal mode for starter metadata:

```bash
SVMC_VAULT_PATH="/absolute/path/to/your/vault" severino-vault-mcp doctor --propose
```

The proposals are intentionally conservative. Review them before pasting into
real docs, especially `sensitivity`.

## 3. Fix One Workflow First

Create or clean these docs first:

- One Quick Index with `doc_id: report-playbook-mcp-index`.
- One real runbook with an `rb-*` ID.
- One infrastructure note with an `infra-*` ID.

Then verify:

```text
vault://labs/quick-index
vault://labs/doc/<your-runbook-id>
find("your operational question")
```

## Reports

A report (`doc_type: report`) is a dated record of one run, written once:
an audit, review, research pass, session or evaluation, named by its
required `kind`. It lives in a `Reports/` folder, and a `Reports/` folder
holds only reports, so `validate` refuses a report anywhere else and any
other doc_type inside one. Moving a vault to this rule:

- A note in `Reports/` that records one run: set `doc_type: report` and its
  `kind` in one `set_frontmatter` call.
- A note there that keeps changing (a plan, a decision record, a scorecard
  that is appended to): move it out with `move_doc`, to the project's notes.
- A note whose work is done and that nothing reads: `move_doc` with
  `archive=true`. The archive keeps it where it lands, `Reports/` or not.

## Bad Doc To Fixed Doc

Before:

```markdown
# Reset DNS Filtering

Restart AdGuard Home and check local resolution.
```

After:

```markdown
---
doc_id: rb-reset-dns-filtering
title: Reset DNS Filtering
doc_type: runbook
system: AdGuard Home
environment: adguard
status: active
sensitivity: internal
tags:
  - dns
  - adguard
related_projects:
  - client-edge-dns
---

# Reset DNS Filtering

Restart AdGuard Home and check local resolution.
```

## Sensitivity Triage

- Use `public` only for docs safe to publish.
- Use `internal` for private operational context that is safe to enter chat.
- Use `sensitive` for private but chat-acceptable information that deserves an
  advisory.
- Use `restricted` for anything that may reveal credentials, private key
  paths, recovery flows, token rotation steps, internal auth procedures, or
  escalation paths.

When in doubt, use `restricted` and loosen later.
