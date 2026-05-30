# AI Safety and Security Model

`severino-vault-mcp` is designed for a local AI assistant using MCP over
stdio. It is not a network service, and it does not expose an HTTP listener.
The MCP host and this server run under the same local user account.

It can also be used with local models running on the operator's Mac. In that
setup, the vault, MCP server, MCP host, and model can all stay on the same
machine, so operational context can be used without sending it to a hosted
model provider. This does not remove the need for sensitivity labels or local
unlock controls.

## Safety Goal

The goal is to ground AI answers in the operator's vault while preventing the
assistant from casually pulling credential-adjacent material into chat context.

The MCP should:

- Route the assistant to the right runbook or infrastructure note.
- Return exact operational docs when they are safe to release.
- Withhold `restricted` bodies by default.
- Require local human authorization before releasing a restricted body.
- Avoid broad full-text search over restricted bodies.
- Keep body content out of audit logs.

## Local Model Usage

Running the MCP against a local model is possible for private operations
vaults:

- Vault reads happen from local disk under the operator's user account.
- MCP traffic stays on stdio between local processes.
- Runbook context can be used without sending markdown bodies to a hosted model
  provider.
- Smaller local models can rely on `get_runbook` and Quick Index hints to
  reduce hallucinated operational steps.

The local-model path is still not a hard sandbox. A trusted local MCP host is
required, and a compromised host can request allowed tools. `restricted`
docs therefore remain withheld unless the explicit local unlock flow succeeds.

## Sensitivity Levels

| Sensitivity | Body behavior |
|---|---|
| `public` | Released. |
| `internal` | Released. |
| `sensitive` | Released with advisory text. |
| `restricted` | Withheld by default. Released only through `read_doc` after explicit request and local unlock. |

`restricted` is for docs near credentials, keys, CA procedures, rotation
steps, or other material that should not casually enter an AI chat transcript.

## Secret-Adjacent Release Flow

The model can request a body, but the local machine authorizes it:

```text
read_doc(doc_id, include_restricted=True)
        |
        v
doc is restricted?
        |
        v
SVMC_ALLOW_RESTRICTED_UNLOCK=1?
        |
        v
unlock hash configured?
        |
        v
hidden macOS local prompt succeeds?
        |
        v
release body for this one request only
```

All conditions must pass:

- The caller explicitly sets `include_restricted=True`.
- The local MCP environment has `SVMC_ALLOW_RESTRICTED_UNLOCK=1`.
- A salted unlock hash is configured through Keychain, a local hash file, or
  `SVMC_RESTRICTED_UNLOCK_HASH`.
- The local hidden-input prompt succeeds.

The unlock phrase must never be typed into AI chat.

## Why the Prompt Is Local

The MCP uses a local hidden-input prompt on macOS. This keeps the unlock phrase
out of:

- the chat transcript
- MCP tool arguments
- model context
- audit logs

If the prompt is unavailable, cancelled, or fails verification, the body stays
withheld and the response includes metadata plus an unlock failure reason.

## Audit Logging

Unlock attempts append one local audit line:

```text
timestamp action=restricted_unlock doc_id=<doc_id> result=<result> client=stdio
```

The audit log never includes:

- doc body content
- unlock phrases
- prompt text entered by the user

The default audit path is:

```text
~/.local/state/severino-vault-mcp/audit.log
```

## Search Safety

`search_body` never searches restricted bodies, even if its deprecated
compatibility flag is set. This is intentional: broad full-text search can leak
too much context from credential-adjacent docs.

Use `read_doc(..., include_restricted=True)` for a specific per-doc unlock
request instead.

## Write Safety

Write tools are intentionally narrow. They do not expose a general "edit this
file" or "run this command" capability.

| Tool | Mutation boundary |
|---|---|
| `add_frontmatter` | Prepends a validated frontmatter block to an existing markdown file under the configured vault root and indexed folders when that file does not already have frontmatter. |
| `update_frontmatter` | Updates allowed fields inside an existing frontmatter block for one indexed vault doc. `doc_id` is immutable. |
| `update_writeup_frontmatter` | Updates scalar frontmatter fields in one `05 Writeups/<slug>/index.md` file: `title`, `description`, `published`, `published_at`, `last_reviewed`, `cover_image`, `featured`, and `featured_order`. |
| `reorder_featured` | Updates only `featured` and `featured_order` across `05 Writeups/<slug>/index.md` files so the featured list stays sequential after insert, move, or unfeature operations. |
| `apply_jseverino_d1_schema` | Applies the fixed `db/schema.sql` from the configured jseverino.com site repo to the configured remote Cloudflare D1 database; requires `confirm=True`. |

Common constraints:

- Vault-file writes reject paths that escape the configured vault root.
- Generic frontmatter writes validate enum fields before touching disk.
- jseverino.com writeup paths and technology-catalog paths must resolve inside
  the configured vault root.
- Writeup frontmatter writes preserve unrelated lines and only change the
  requested scalar keys.
- `reorder_featured` reports every writeup it changed and the resulting
  featured order.
- `apply_jseverino_d1_schema` is not an arbitrary SQL runner; it applies one
  known schema file to one configured database.

Markdown body edits are not exposed as a broad MCP write tool.

## Remaining Trust Assumptions

This is a local MCP. It assumes:

- The local operating system account is trusted.
- The MCP host process is trusted enough to invoke local tools.
- The vault files are readable by the local user.
- A compromised MCP host can still request tools; the local unlock prompt is
  the boundary for `restricted` body release.

This project reduces accidental AI exposure. It is not a substitute for
filesystem permissions, Keychain hygiene, or vault-level secret management.
