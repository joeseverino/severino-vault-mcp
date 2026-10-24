# AGENTS.md: house rules for severino-vault-mcp

`CLAUDE.md` is a symlink to this file.

One Go binary: a stdio MCP server for every vault (labs, edu, and any vault a
provider adds) plus a CLI over the labs vault. Go 1.27, the official MCP SDK, and one
TOML parser; nothing else.

## Principles

- Use what exists before building: a platform built-in (Claude Code, MCP, Go's
  standard library, mise), a proven tool, or a primitive already in this repo.
  Build only what nothing proven does, and say what was checked.
- Prefer an open standard over a house format wherever one fits: MCP, JSON
  Schema, AIP-161 field masks, Agent Skills, Obsidian Tasks and Dataview fields,
  POSIX locks, ISO 8601. A house format needs a reason no standard covers.
- Decide formats before data exists: changing a shape after notes carry it is a
  migration.
- Emit once, derive everywhere: each fact, rule or shape has one source here
  (`schema` for field rules and the settable frontmatter fields, `fsx.Vault.Update` for writes, `core/fields.go` for
  masks, `core/next.go` for next steps, the command table for CLI surfaces) and
  everything else derives from it. A second copy is a defect; remove it and add
  the guard that stops a third.

## Shape

```
cmd/severino-vault-mcp   main: cli.Main
internal/cli             command table -> parsing, --help, describe; one runner per subcommand
internal/mcpserver       the server: core tools, resources, provider passthrough
internal/vaults          composes vaults: labs, edu (config present), providers
internal/provider        connects a provider: profile resource, tools, instructions
internal/core            the 9 shared tools, transport-free (the MCP adapter and tests both call these)
internal/{vault,sections,search,query,frontmatter,schema,gate,write,tasks,queue,daily,brief,doctor,education,hqmanifest}
                         the governance engine
internal/{config,fsx,jsonx,pystr,clock,contracts,tabular}  shared plumbing
```

- HQ, the site and the golden files read this output byte for byte: `jsonx`
  keeps key order and `ensure_ascii`, `pystr` formats the lists in error
  messages, and the frontmatter parser is a constrained YAML subset (numbers
  and dates stay strings). Don't swap in a YAML library or `encoding/json` for
  these.
- Every service returns one `*jsonx.Obj`; failures are `{"ok": false, "error": "..."}`.
  CLI subcommands exit 0/1 on `ok`, 2 on argument errors.
- The labs profile is `schema.Labs`. `schema --json` is the frozen shape HQ
  commits (`docs_index/schema.json`); `tests/golden/schema.json` pins it.
- No private domain lives here, not even by name. Providers are declared only
  in the operator's config (`[[providers]]`); the vault name, profile, tools
  and instructions arrive at runtime. Tests use a fake provider fixture.

## Contracts

- Consumers run the installed binary. `--fingerprint` (checked by `tools doctor`)
  hashes the embedded Go sources; `go run ./cmd/severino-vault-mcp --fingerprint`
  is the source side.
- `export education` JSON is read by jseverino.com (`bin/content-sync`) and
  resume-engine (`lib/reconcile-coursework`). Changing its shape is a contract
  change for both.
- `validate <path>...` is meant for editor and agent hooks that check every
  edited path: `<relative_path>: <problem>` lines and exit 1, silent exit 0 for paths
  no local vault indexes, exit 2 on usage. Its problems are `schema.Profile.Check`,
  the rules set_frontmatter writes by.
- `skills/vault/SKILL.md` is how agents use the tools; the server's
  instructions only point to it. Adding or cutting a tool changes the skill
  too (`TestTheVaultSkillNamesEveryRegisteredTool`). The repo root is a Claude
  Code plugin (`.claude-plugin/plugin.json`, `.mcp.json`).
- `tests/golden/`: `schema.json`, `labs.schema.json` (`schema --json-schema`,
  the interface for off-the-shelf validators and editors), `cli-describe.json`,
  `mcp-tools.txt`. A diff there is a deliberate contract change; regenerate
  with the binary. The JSON Schema derives from `Profile.Settable`; a test
  holds it to `Profile.Check`'s verdict on every fixture doc.
- The note lock (`fsx.LockPath`, documented for outside writers in
  `docs/ai-tool-contract.md`): any process that writes a vault note, such as
  an append-only hook, takes an exclusive `flock` on
  `<vault>/.locks/<vault-relative path, "/" as "%">.lock` for the whole write.
  Changing the path rule is a contract change for every such writer.
- The work queue (`queue` tool and CLI, `docs/ai-tool-contract.md#work-queue`):
  the item line grammar in `internal/queue`, the `queue_prefix` and `repos`
  keys of a project's `index.md`, and the CLI's flags and compact JSON are
  read by session and worker hooks and by HQ. Changing any of them is a
  contract change for those callers; keep `Parse(Format(item)) == item`.

## Safety

- `public`/`internal`/`sensitive` bodies are released; `restricted` is withheld
  unless the caller asks and the local unlock succeeds. Text search never reads
  restricted bodies.
- Writes are schema-specific: validate against the vault's profile, keep
  `doc_id` immutable, serialize with `frontmatter.Serialize`, and change an
  existing note only through `fsx.Vault.Update` (`AtomicCreate` for a new
  one, `Move` to relocate one, under both notes' locks). Update holds the note lock from read to rename and re-reads before
  the rename, re-running the edit if a writer that skipped the lock appended,
  so its edit function may run more than once. Everything is confined to the
  vault by `os.Root`. Never write vault files any other way:
  `TestOnlyFsxWritesFiles` fails on an `os` file write outside `fsx`.
- A doc's body is written through `write_doc` (`core.WriteDoc`, CLI
  `write-doc`): create checks the new block with `Profile.Check`, so a created
  doc passes `validate`; section edits change no frontmatter but `last_reviewed`.

## Verify

```bash
scripts/check.sh            # the CI gate: gofmt, vet, test -race, govulncheck, golangci-lint, repo invariants
bash tests/golden/verify.sh
scripts/evals.sh --misses   # retrieval eval; SVMC_EVAL_VAULT=<vault> scores the real vault with evals/local/cases.json
```

- Run the eval before changing ranking, `find` output or tool descriptions,
  and raise `TestEvalFloors` when the numbers improve. Decide on the real-vault
  run: the fixture is a regression floor, too small to compare backends.
- The repository-policy check refuses file names that look like sync conflict
  copies (ending in ` <number>.md`); name fixtures accordingly.

## Test idioms

- Hermetic: `internal/testkit` builds throwaway vaults (`FakeVault`,
  `MultisectionDoc`) and an env (`Env`) that points edu at a missing config
  and declares no providers, so no test reads this machine's real vaults.
- `mcpserver` tests drive the real server through an in-memory client; the
  provider seam is tested against the test binary re-executed as a provider.
- Seams: `gate.Approve` (the 1Password read behind the restricted unlock;
  no test runs the real `op`), `clock.Now`, `fsx.BeforeReplace` (the window
  between a rewrite's read and its rename; `testkit.AppendDuringWrite` lands
  an outside append there), `fsx.LockWait`. A failed atomic write is
  simulated with a read-only directory.
- Compare a written body byte for byte as `text[frontmatter.BodyOffset(text):]`;
  `frontmatter.Split` trims the trailing newline.
