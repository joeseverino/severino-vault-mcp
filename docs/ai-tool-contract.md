# AI Tool Contract

The short operating contract for AI clients. More directive than the human
docs: the goal is correct tool selection with small responses. Architecture:
[`architecture.md`](architecture.md).

## Core rule

Use tools before prose. Don't answer an operational question from model memory
when a vault doc exists. Every shared tool takes `vault` (`labs` by default,
`edu` and each provider's vault when configured).

## Routing

| User intent | First tool/resource | Then |
|---|---|---|
| Broad process question | `vault://labs/quick-index` | Read the target `vault://labs/doc/{doc_id}`. |
| Specific runbook question | `find` | `read_doc` on the top hit (with its `section` when the hit names one); quote commands exactly. |
| Known doc ID | `read_doc` or `vault://{vault}/doc/{doc_id}` | Respect the sensitivity policy. |
| System context | `find(by="system")` | Read the selected doc before summarizing. |
| Docs for a project | `find(by="project")` | Read before summarizing. |
| Body search | `find(by="text")` | Snippets are for discovery; read the doc before final instructions. |
| Daily progress or log | `daily_progress` | Summarize from the returned note. |
| Missing or stale metadata | `set_frontmatter` | Report changed fields and the next sync step. |
| Tasks | `task_board`, then `task_write` | `task_write(action=...)`: add, status, promote, delete. |
| Duplicate doc ID response | `doctor` (CLI) | Report every conflicting path; don't pick one. |

## Sensitivity

| Sensitivity | AI behavior |
|---|---|
| `public` / `internal` | Use the body normally. |
| `sensitive` | Use the body and mention the advisory. |
| `restricted` | Don't ask for the body unless the user explicitly needs it. `read_doc(..., include_restricted=True)` only for one specific doc. |

`find(by="text")` never searches restricted bodies.

## Shell / CLI

The same retrieval as console subcommands, emitting the same `{ok, ...}` JSON:

| Need | Command |
|---|---|
| The command surface | `severino-vault-mcp describe` |
| Ranked section menu | `severino-vault-mcp find <query>` |
| One section or a whole body | `severino-vault-mcp read <doc_id> [--section <slug>]` |
| The edu dataset | `severino-vault-mcp export education` |

`describe` is generated from the CLI's command table and emits a
[Cordon v4](https://github.com/joeseverino/cordon) contract; prefer it over
restating commands from this doc.

## Response discipline

- A short runbook gets a short answer.
- Quote commands exactly.
- If no matching doc exists, say so before offering general guidance.
- For writes, report only the changed fields and any follow-up.
