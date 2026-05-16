# severino-knowledge-router

Local MCP server that routes an AI assistant to the right doc in the Severino
Labs vault and Severino HQ. Reads vault frontmatter directly from disk;
enforces the documented sensitivity policy so secret-adjacent material never
leaks into an LLM context window.

Runs on the Mac as a stdio server. No network exposure.

---

## What it does

Seven tools, registered with any MCP host (Claude Code, Claude Desktop, Cline,
etc.):

| Tool | Read or write | What it answers |
|---|---|---|
| `find_runbook(query, limit=5)` | read | "How do I add an NPM proxy host?" |
| `lookup_system(name)` | read | "Tell me about AdGuard Home" |
| `read_doc(doc_id)` | read | Returns the markdown body — but only for `public`/`internal` docs. `sensitive` returns metadata only; `secret_adjacent` refuses with a pointer. |
| `inventory_for_project(slug)` | read | "What docs are part of homelab-dns?" |
| `recent_changes(days=7)` | read | Recent vault commits within indexed folders |
| `add_frontmatter(...)` | write | Prepends a fully-validated frontmatter block to a vault doc that doesn't have one. Refuses if frontmatter already exists. |
| `update_frontmatter(...)` | write | Updates fields on a doc that already has frontmatter. `doc_id` is immutable; everything else is fair game. `touch_last_reviewed=True` is the common "I just re-read this runbook" pattern. |

All seven respect the schema documented at
`02 Infrastructure/Severino HQ/Frontmatter Schema.md` in the vault.

---

## Why it exists

Severino Labs has three layers:

- **Obsidian vault** — the deep knowledge (runbooks, infra docs, decision records).
- **Severino HQ** — the operational ledger (assets, expenses, receipts, projects).
- **`severino-knowledge-router`** (this MCP) — the bridge.

Every vault `.md` under `01 Projects/`, `02 Infrastructure/`, `03 Runbooks/`
has a YAML frontmatter block with `doc_id`, `system`, `sensitivity`, `tags`,
etc. HQ already syncs that frontmatter via `hq sync`; this MCP exposes the
same data to any LLM that wants to ground a question against the vault.

The sensitivity gate is the policy boundary: `secret_adjacent` docs can be
searched and pointed at, but their bodies never enter the LLM context.

---

## Install

Requires Python 3.11+. Uses `uv` to manage the install.

```bash
git clone git@github.com:joeseverino/severino-knowledge-router.git \
    ~/Documents/Code/Assets/severino-knowledge-router
cd ~/Documents/Code/Assets/severino-knowledge-router
uv sync --extra dev
uv run pytest
```

For day-to-day use, install as a `uv` tool so the `severino-knowledge-router`
command lands on `$PATH`:

```bash
uv tool install --from . severino-knowledge-router
```

Re-run after `git pull` with `uv tool upgrade severino-knowledge-router`.

### Wire to Claude Code

```bash
claude mcp add severino-knowledge-router severino-knowledge-router
```

Or, if you didn't `uv tool install`, point Claude Code at the local checkout:

```bash
claude mcp add severino-knowledge-router \
    --command uv \
    --args run --directory $HOME/Documents/Code/Assets/severino-knowledge-router severino-knowledge-router
```

### Wire to Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "severino-knowledge-router": {
      "command": "severino-knowledge-router",
      "env": {
        "SKR_VAULT_PATH": "/Users/josephseverino/Documents/Code/Severino Labs"
      }
    }
  }
}
```

Restart the Claude Desktop app. The 7 tools should appear in the MCP picker.

---

## Configuration (env vars)

| Var | Default | Purpose |
|---|---|---|
| `SKR_VAULT_PATH` | `~/Documents/Code/Severino Labs` | Vault root |
| `SKR_INDEXED_DIRS` | `01 Projects:02 Infrastructure:03 Runbooks` | Colon-separated subdirs the loader recurses into |
| `SKR_HQ_URL` | `https://hq.jseverino.com` | Reserved for the future HQ integration |
| `SKR_CACHE_SECONDS` | `30` | How long the in-memory vault index stays warm |

The package is single-user by design — there's no auth, no remote surface, no
shared state. It runs in the same process as your MCP host and reads files
your user account can read.

---

## Schema contract

The frontmatter spec lives in the vault at
`02 Infrastructure/Severino HQ/Frontmatter Schema.md`. The MCP refuses to
write anything that doesn't conform — `doc_type`, `environment`, `status`,
and `sensitivity` are all enum-validated; `doc_id` is checked for one of the
known prefixes (`rb-`, `infra-`, `report-`, `project-`, `note-`).

---

## Sensitivity policy

| Sensitivity | `read_doc` returns |
|---|---|
| `public` | Full body |
| `internal` | Full body |
| `sensitive` | Metadata only + policy note |
| `secret_adjacent` | Metadata only + policy note (refuses with stronger language) |

`secret_adjacent` is the marker for anything adjacent to credentials, CA
keys, plaintext rotation procedures, etc. The tool will tell the LLM where
the doc lives in Obsidian; it will not return the body.

---

## License

MIT — see `LICENSE`.

## Status

v0.1.0. Read tools + frontmatter write tools complete. HQ JSON integration
(assets, expenses, projects) deferred to v0.2 once HQ has a proper API token.
