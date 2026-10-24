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
- Smaller local models can rely on `find` and its Quick Index hints to
  reduce hallucinated operational steps.

The local-model path is still not a hard sandbox. A trusted local MCP host is
required, and a compromised host can request allowed tools. `restricted`
docs therefore remain withheld unless the unlock below succeeds.

## Sensitivity Levels

| Sensitivity | Body behavior |
|---|---|
| `public` | Released. |
| `internal` | Released. |
| `sensitive` | Released with advisory text. |
| `restricted` | Metadata only. The body is released for one read, only through `read_doc` after an explicit request and the unlock below. |

`restricted` is for docs near credentials, keys, CA procedures, rotation
steps, or other material that should not casually enter an AI chat transcript.

## Secret-Adjacent Release Flow

The unlock is macOS only: it needs the 1Password desktop app (Touch ID) and the
`op` CLI. On other platforms restricted bodies stay withheld.

The model can request a body, but the local machine authorizes it:

```text
read_doc(doc_id, include_restricted=True)
        |
        v
doc is restricted?
        |
        v
restricted.unlock_ref configured?
        |
        v
no OP_SERVICE_ACCOUNT_TOKEN in the server's environment?
        |
        v
`op read` of unlock_ref approved with Touch ID?
        |
        v
release body for this one request only
```

All conditions must pass:

- The caller explicitly sets `include_restricted=True`.
- `[restricted].unlock_ref` (or `SVMC_RESTRICTED_UNLOCK_REF`) names a
  dedicated 1Password item, kept in a vault no service account can read.
  Unset, restricted bodies are always withheld.
- The server's environment has no `OP_SERVICE_ACCOUNT_TOKEN`. A service
  account would approve its own unlock, so its presence refuses every unlock.
- `op read --no-newline <unlock_ref>` exits 0 with a non-empty value. The
  1Password desktop app integration asks for Touch ID on every read. The
  child process gets no service-account or Connect credentials, the value is
  discarded at once, and the read times out after 60 seconds.

## Why the Approval Is Touch ID

The approval is a fingerprint on the Mac, so nothing secret passes through:

- the chat transcript
- MCP tool arguments
- model context
- audit logs

An agent can ask for a restricted body, but only the person at the Mac can
approve it. If 1Password is locked, the CLI is missing, the prompt is
cancelled, or the read fails, the body stays withheld and the response
includes metadata plus the reason and the next step.

## Audit Logging

Unlock attempts and PII releases append one local audit line:

```text
timestamp action=restricted_unlock doc_id=<doc_id> result=<result> client=stdio
timestamp action=contact_pii_access rows=<n> client=stdio
timestamp action=csp_pii_access rows=<n> client=stdio
```

The audit log never includes:

- doc body content
- the value read from the unlock item
- contact PII (only the row count of a release is recorded)

The default audit path is:

```text
~/.local/state/severino-vault-mcp/audit.log
```

## Search Safety

`find(by="text")` never searches restricted bodies. Broad full-text search can
leak too much context from credential-adjacent docs.

Use `read_doc(..., include_restricted=True)` for a specific per-doc unlock
request instead.

## Write Safety

Write tools are intentionally narrow. They do not expose a general "edit this
file" or "run this command" capability.

| Tool | Mutation boundary |
|---|---|
| `set_frontmatter` | One markdown file under the named vault's root and indexed folders. Creates a block when none exists (doc_id, title, doc_type, system required) or updates allowed fields in place. `doc_id` is immutable. |
| `write_doc` | One registered doc under the named vault's root and indexed folders: create it (refuses an existing file), or replace or append one section. Frontmatter validates against that vault's schema; restricted docs need a Touch ID approval through 1Password. |
| `move_doc` | One registered doc, or its folder with `with_folder`, moved to a new path under the named vault's indexed folders, outside the read-only ones, under both notes' locks. Never replaces a file and never rewrites another note; the bytes of the doc don't change. |
| `log_entries` | One registered log note: `close` appends ` → <resolution>` to one open entry's line, through `write_doc`'s validated atomic write. Restricted notes are refused; notes in read-only folders can be listed but not closed. |
| `queue` | One project's `Queue.md` (or `07 Backlog/Queue.md`): a new note through `write_doc`'s create, then each change rewrites item lines under the note's lock, through the validated body write. A body doc through the same create. Restricted queues are refused and skipped by `list`. |
| Provider tools | Governed by the provider process; the host forwards calls unchanged and never widens them. |

Common constraints:

- Every call acts on the vault named by its `vault` argument, through that
  vault's context. One vault's files are never reachable through another's.
- Writes reject paths that escape the vault root, by `..` or by a symlink
  that leaves it. Reads and writes of vault files go through an `os.Root`
  (`internal/fsx`), so a symlink swapped in after a check cannot redirect them.
  Scalar escaping (`frontmatter.Escape`) and the rewrite of an existing note
  (`fsx.Vault.Update`, under the note's lock) each have one implementation.
- Every write reports failure with the same `{"ok": false, "error": "..."}`
  envelope.
- Frontmatter writes validate against the vault's schema profile before
  touching disk, and replace atomically: a failed write never truncates the
  original.
- Duplicate `doc_id` values are excluded from reads and searches until fixed.

Markdown body edits are not exposed as a broad MCP write tool.

## Remaining Trust Assumptions

This is a local MCP. It assumes:

- The local operating system account is trusted.
- The MCP host process is trusted enough to invoke local tools.
- The vault files are readable by the local user.
- A compromised MCP host can still request tools; the Touch ID approval is
  the boundary for `restricted` body release.

This project reduces accidental AI exposure. It is not a substitute for
filesystem permissions, password manager hygiene, or vault-level secret management.
