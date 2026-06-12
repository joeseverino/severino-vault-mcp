# AI Tool Contract

This is the short operating contract for AI clients using
`severino-vault-mcp`. It is intentionally more directive than the human docs:
the goal is fast, correct tool selection with small responses.

For the human architecture overview, read [`architecture.md`](architecture.md).
For the real operator workflow inventory, read
[`operator-workflows.md`](operator-workflows.md).

## Core Rule

Use tools before prose. Do not answer operational questions from model memory
when a vault doc or workflow tool exists.

## Generic Vault Routing

| User intent | First tool/resource | Then |
|---|---|---|
| Broad process question | `vault://quick-index` | Read the target `vault://doc/{doc_id}`. |
| Specific runbook question | `find_runbook` or `get_runbook` | Read the top hit if needed; quote commands exactly. |
| Known doc ID | `vault://doc/{doc_id}` or `read_doc` | Respect sensitivity policy. |
| System context | `lookup_system` | Read the selected doc before summarizing. |
| Body search | `search_body` | Use returned snippets only as discovery; read the doc before final instructions. |
| Missing metadata | `add_frontmatter` or `update_frontmatter` | Report changed fields and next sync step. |
| Duplicate doc ID response | `doctor` | Do not choose one path; report every conflict and require repair. |

## Sensitivity Handling

| Sensitivity | AI behavior |
|---|---|
| `public` / `internal` | Use the body normally. |
| `sensitive` | Use the body and mention the advisory. |
| `restricted` | Do not ask for the body unless the user explicitly needs it. Use `read_doc(..., include_restricted=True)` only for a specific doc, never broad search. |

`search_body` excludes restricted bodies by design.

## jseverino.com Fast Path

| User intent | Tool |
|---|---|
| What is the featured/home writeup order | `list_featured_writeup_order` |
| Which writeups exist, are published, are drafts, or are featured | `list_writeups(filter)` |
| What is the featured order | `list_writeups("featured")` |
| Does a technology slug exist or belong in the home cloud | `get_technology_catalog` and `find_writeups_using_tag` |
| Is a writeup ready to publish | `prepare_writeup_publish(slug)` |
| Need detailed blockers for one writeup | `validate_writeup(slug)` |
| Need summaries and readiness for every writeup | `writeup_dashboard()` |
| Flip publish state, date, cover, title, description, or review date | `update_writeup_frontmatter(slug, ...)` |
| Insert, move, or unfeature a featured writeup | `reorder_featured(slug, position)` |
| Apply several edits and a complete featured order | `apply_writeup_plan(plan)` |
| Review contact form state | `list_contact_submissions` |
| Review CSP report state | `list_csp_reports` or `count_csp_reports` |
| Check live headers | `check_jseverino_security_headers(path)` |

Do not grep writeup frontmatter, hand-parse `_technology-groups.md`, or edit
featured order manually. The dedicated tools are faster and preserve invariants.

## Response Discipline

- If a runbook is short, answer short.
- Quote commands exactly from docs.
- If no matching doc exists, say that before offering general guidance.
- Use `prepare_writeup_publish(slug)` by default; enable tag usage only when
  making a tag-promotion decision.
- Use `writeup_dashboard()` when a client needs both inventory and validation;
  do not issue separate list and batch-validation calls.
- Use `apply_writeup_plan(plan)` for multi-writeup interactive saves so all
  affected files commit together or roll back together.
- For write tools, report only the changed fields and any required follow-up.
