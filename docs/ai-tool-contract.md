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
| Specific runbook question | `find` | `read_doc` on the top hit with `sections=[<the hit's section>]`; the whole body when the section isn't enough. Quote commands exactly. |
| Known doc ID | `read_doc` or `vault://{vault}/doc/{doc_id}` | Respect the sensitivity policy. |
| System context | `find(query, system="...")` | Read the selected doc before summarizing. |
| Docs for a project | `find(query, project="...")` | Read before summarizing. |
| Body search | `find(by="text")` | Snippets are for discovery; read the doc before final instructions. |
| New doc, or a section edit | `write_doc` | Body plus frontmatter in one validated call; never edit the file directly. |
| Missing or stale metadata | `set_frontmatter` | Report changed fields and the next sync step. |
| Tasks | `task_board`, then `task_write` | `task_write(action=...)`: add, status, promote, delete; `items=[...]` for several changes in one call. |
| Open entries in a log note | `log_entries(doc_id, contains=...)` | `log_entries(action="close", match=..., resolution=...)` closes one; never read or edit the log whole. |
| Duplicate doc ID response | `doctor` (CLI) | Report every conflicting path; don't pick one. |

## Failures

Every failure is `{"ok": false, "error": "..."}` with `next_steps`: short
strings, each starting with a call to a registered tool, such as
`find(query="...")` for an unknown doc_id or
`read_doc(doc_id="...", fields=["outline"])` for a heading that isn't there.
`read_doc`'s not-found and withheld answers, and a `find` with no hits, carry
them too. An argument the schema refuses (an unknown name, a wrong type, a
value outside its enum, a missing required one) fails the same way, flagged
as a tool error. Take a next step before guessing.

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
| Some sections or a whole body | `severino-vault-mcp read <doc_id> [--sections <slug> ...] [--fields outline]` |
| Open log entries, or close one | `severino-vault-mcp log-entries list <doc_id> [--contains <text>]`, `log-entries close <doc_id> --match <text> --resolution <text>` |
| The edu dataset | `severino-vault-mcp export education` |

`describe` is generated from the CLI's command table and emits a
[Cordon v4](https://github.com/joeseverino/cordon) contract; prefer it over
restating commands from this doc.

## Writing a note from outside the server

The server rewrites a note by reading it, editing, and renaming a new file
over it. A process that writes the same note directly, such as an
append-only hook adding a line, takes the note's lock for the duration of its
append, so the server's rewrite never drops that line:

- Lock file: `<vault root>/.locks/<path of the note relative to the vault
  root, clean (no `./`, `..` or doubled `/`), every "/" replaced by
  "%">.lock`. `Projects/Example/queue.md` locks
  `.locks/Projects%Example%queue.md.lock`. Create `.locks/` and the file if
  absent; never delete them.
- Lock: an exclusive `flock(2)` on that file, held from before the note is
  opened until after it is closed. Never lock the note itself: a rewrite
  replaces its inode, so a lock on the old one guards nothing.
- Write with `O_APPEND`, and keep the hold short: the server waits up to ten
  seconds for the lock, then fails the write with a retry as its next step.

```go
lock, _ := os.OpenFile(filepath.Join(vault, ".locks", strings.ReplaceAll(rel, "/", "%")+".lock"), os.O_RDWR|os.O_CREATE, 0o644)
defer lock.Close() // closing releases the lock
syscall.Flock(int(lock.Fd()), syscall.LOCK_EX)
note, _ := os.OpenFile(filepath.Join(vault, rel), os.O_WRONLY|os.O_APPEND, 0)
note.WriteString(line); note.Close()
```

On Linux, `flock <lock file> sh -c 'printf ... >> <note>'` does the same
from a shell. A writer that skips the lock is still caught when its append
lands before the server's final re-read: the server re-applies its edit over
the new bytes. Only the lock closes the last window.

## Response discipline

- A short runbook gets a short answer.
- Quote commands exactly.
- If no matching doc exists, say so before offering general guidance.
- For writes, report only the changed fields and any follow-up.
