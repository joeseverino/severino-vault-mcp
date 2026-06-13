# Design: Federated, Section-Scoped Retrieval

> **Status: Proposal (2026-06-13). Not yet implemented.** This doc locks the
> decisions; nothing here ships until a phase below is built and tested. Edit
> this doc, not the code, until a phase is agreed.

## Problem

A fact about a repo (the `hq ship` behavior, the 25 tool names, a gate list)
lives in that repo. Today the MCP indexes **only the vault**, so the only way
an AI session can recall a repo fact is if a human first copied it into a vault
doc. That copy is the staleness engine: code changes → repo doc changes → and
the vault copy rots until someone notices (the 2026-06-13 sweep fixed eight such
copies at once).

Root cause is a missing **source boundary**, not a missing tool. The MCP doc
that listed 8 of 25 tools wasn't under-maintained — it was restating something
the code can describe about itself.

## Principles

1. **One owner per fact.** A fact is authored in exactly one place; everywhere
   else *points* at it. No restating across systems.
2. **Retrieval returns the smallest correct span.** The unit of return is a
   section, not a file. (This is RULE 2 — "match the doc's terseness" — applied
   to retrieval granularity.)
3. **Federation is token-neutral.** Adding repos to the index changes local
   build time and ranking sharpness, never the per-query token cost — retrieval
   returns the same small span regardless of corpus size.
4. **Structured facts come from emitted output, not prose.** `--help`,
   `schema --json`, a tool manifest — the code emits exactly the fact.

## The boundary (the load-bearing decision)

| Owner | Owns | Surfaced as |
|---|---|---|
| **Vault** | Infra topology, runbooks, decisions, the *why* — human-authored knowledge no repo owns | `.md` + frontmatter (today) |
| **Each repo** | Its own mechanics: CLI surface, schema, deploy model, changelog | `AGENTS.md`, `README`, `docs/**`, `CHANGELOG` (already maintained in every repo) |
| **Code** | Structured facts: command help, enum contracts, tool lists | emitted (`--help`, `schema --json`, `--fingerprint`) — already done for schema |

A vault doc may **point at** a repo fact; it may not restate it. The rare doc
that must restate (a human narrative around a code fact) gets a drift guard
(below).

## Design

Three capabilities, smallest blast radius first. Each is independently
shippable and testable against the fixture vault.

### 1. Section-scoped index (biggest win, no federation yet)

Today `vault.py` produces one `Doc` per file with a single `body`. Add a
section view: parse the markdown heading tree and attach a list of `Section`
spans to each `Doc`, each carrying its heading path
(`"Routine operations > Backing commands"`), text span, start line, and the
doc's inherited frontmatter (`doc_id`, `sensitivity`, provenance).

- `search.py:rank` scores at section granularity and returns the best section(s),
  not whole docs.
- `read_doc(doc_id, section=…)` returns one section; whole-doc read stays
  available behind an explicit flag.

Token effect, concrete: a full `read_doc` on the MCP doc is ~230 lines
(~2–3k tokens). The matched section is ~12–40 lines (~150–400 tokens). The
search "menu" line is ~20 tokens. A typical answer drops from ~2.5k → ~300
tokens, **before** federation enters the picture.

### 2. Two-tier return: menu, then one bite

- **Search/find** returns a cheap menu: `doc_id`/source, heading, a one-line
  summary (frontmatter `description` or the section's first sentence),
  provenance, score. **No body.**
- **Read** returns exactly one section.

`get_runbook` (search+read in one call) stays the small-model path; it just
selects and returns one *section* instead of a whole body.

### 3. Federation + provenance

`config.py` gains `repo_sources`: a list of `{name, path, globs,
sensitivity_default}`. The loader indexes those files alongside the vault,
tagging every `Doc`/`Section` with `source` (`vault` | `repo:<name>`) and a
`source_ref` (relative path; optionally the git commit). Repo docs lack
frontmatter, so synthesize metadata the same way `_build` already does for
`04 Reference` docs (`type: reference` → `ref-<slug>`): derive a `doc_id`,
apply the source's `sensitivity_default`.

Default sources per repo (already present everywhere): `AGENTS.md`, `README.md`,
`docs/**/*.md`, `CHANGELOG.md`. **Not** the code source tree — only
model-facing docs and emitted facts.

### Ranking: dedup by owner

With the boundary enforced, true duplicates are rare. When sources still
collide, break ties by: declared owner for the topic → most recently changed
authoritative source → vault over repo for infra/runbook intents, repo over
vault for mechanics intents. Returning the **one** authoritative hit is both a
correctness win and a token win (no 3× contradictory spans).

### Drift guards (generalize `schema --check-doc`)

`schema --check-doc` already proves the pattern: a doc that restates a code
fact gets a CI check that fails on drift. Generalize to a `check-doc` that
verifies a doc's restated-fact block against its emitted source. This is the
escape hatch for the rare narrative-over-code-fact doc — it stays human-written
but can't silently rot.

## Non-goals

- **No embeddings / vector store.** Keyword + section scoring is enough at this
  scale; the committed eval (`scripts/eval_ranking.py`) is the trigger — revisit
  only if it regresses on real queries.
- **No indexing of code source files.** Only model-facing docs + emitted facts.
- **No auto-copying repo docs into the vault.** That *is* the copy step we're
  deleting.
- **Narrative and "why" stay human-authored in the vault.** Federation is for
  mechanics, not judgment.

## Sensitivity

The existing gate (`sensitivity.py`) applies unchanged. Federated repo docs get
the source's `sensitivity_default` (e.g. `internal` for a private repo,
`public` for a public one); a federated hit never bypasses the gate, and
provenance in the response makes the source auditable.

## Rollout

- **P0** — this doc; decisions locked.
- **P1** — section chunking + section-aware `find`/`read` over the **vault
  only**. Immediate token win, fully testable on the fixture vault, no
  federation risk.
- **P2** — `repo_sources` config + federate the four default surfaces,
  provenance-tagged, with sensitivity defaults.
- **P3** — dedup-by-owner ranking + recency; the generalized `check-doc` drift
  guard.
- **P4 (optional)** — a `describe` tool/subcommand that returns emitted
  `--help` / `schema --json` slices, so structured-fact questions answer from
  the code's own output.

## Open decisions (resolve before P1)

1. **Section granularity** — split at H2 only (keep H3+ inside), with a
   size cap that sub-splits oversized sections? (Recommended.)
2. **Section addressing** — how `read_doc` names a section: heading slug,
   ordinal, or path string?
3. **Menu summary source** — frontmatter `description` (add the field?) vs.
   first sentence of the section.
4. **Provenance shape** in tool responses (`source`, `source_ref`, commit?).
5. **`sensitivity_default` per source** — confirm private-repo docs default to
   `internal`, public-repo docs to `public`.
6. **Backward compatibility** — keep whole-doc `read_doc` behind a flag; keep
   the current response keys so the `site`/`hq` CLIs and the TUI don't break.
