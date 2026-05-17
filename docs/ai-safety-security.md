# AI Safety and Security Model

`severino-knowledge-router` is designed for a local AI assistant using MCP over
stdio. It is not a network service, and it does not expose an HTTP listener.
The MCP host and this server run under the same local user account.

## Safety Goal

The goal is to ground AI answers in the operator's vault while preventing the
assistant from casually pulling credential-adjacent material into chat context.

The MCP should:

- Route the assistant to the right runbook or infrastructure note.
- Return exact operational docs when they are safe to release.
- Withhold `secret_adjacent` bodies by default.
- Require local human authorization before releasing a secret-adjacent body.
- Avoid broad full-text search over secret-adjacent bodies.
- Keep body content out of audit logs.

## Sensitivity Levels

| Sensitivity | Body behavior |
|---|---|
| `public` | Released. |
| `internal` | Released. |
| `sensitive` | Released with advisory text. |
| `secret_adjacent` | Withheld by default. Released only through `read_doc` after explicit request and local unlock. |

`secret_adjacent` is for docs near credentials, keys, CA procedures, rotation
steps, or other material that should not casually enter an AI chat transcript.

## Secret-Adjacent Release Flow

The model can request a body, but the local machine authorizes it:

```text
read_doc(doc_id, include_secret_adjacent=True)
        |
        v
doc is secret_adjacent?
        |
        v
SKR_ALLOW_SECRET_ADJACENT_UNLOCK=1?
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

- The caller explicitly sets `include_secret_adjacent=True`.
- The local MCP environment has `SKR_ALLOW_SECRET_ADJACENT_UNLOCK=1`.
- A salted unlock hash is configured through Keychain, a local hash file, or
  `SKR_SECRET_ADJACENT_UNLOCK_HASH`.
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
timestamp action=secret_adjacent_unlock doc_id=<doc_id> result=<result> client=stdio
```

The audit log never includes:

- doc body content
- unlock phrases
- prompt text entered by the user

The default audit path is:

```text
~/.local/state/severino-knowledge-router/audit.log
```

## Search Safety

`search_body` never searches secret-adjacent bodies, even if its deprecated
compatibility flag is set. This is intentional: broad full-text search can leak
too much context from credential-adjacent docs.

Use `read_doc(..., include_secret_adjacent=True)` for a specific per-doc unlock
request instead.

## Write Safety

Write tools are intentionally narrow:

- `add_frontmatter` only prepends validated frontmatter to an existing vault doc
  that does not already have frontmatter.
- `update_frontmatter` only updates fields in an existing frontmatter block.
- Both validate enum fields.
- Both reject paths that escape the configured vault root.
- `doc_id` is immutable on updates.

Markdown body edits are not exposed as a broad MCP write tool.

## Remaining Trust Assumptions

This is a local MCP. It assumes:

- The local operating system account is trusted.
- The MCP host process is trusted enough to invoke local tools.
- The vault files are readable by the local user.
- A compromised MCP host can still request tools; the local unlock prompt is
  the boundary for `secret_adjacent` body release.

This project reduces accidental AI exposure. It is not a substitute for
filesystem permissions, Keychain hygiene, or vault-level secret management.
