# severino-knowledge-router

[![CI](https://github.com/joeseverino/severino-knowledge-router/actions/workflows/ci.yml/badge.svg)](https://github.com/joeseverino/severino-knowledge-router/actions/workflows/ci.yml)
![Python](https://img.shields.io/badge/python-3.11%20%7C%203.12%20%7C%203.13-blue)
![MCP](https://img.shields.io/badge/MCP-stdio%20server-green)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Local MCP server that routes an AI assistant to the right doc in the Severino
Labs vault and Severino HQ. Reads vault frontmatter directly from disk;
enforces the documented sensitivity policy so secret-adjacent material never
leaks into an LLM context window.

The same pattern works for any Obsidian-style operational vault with stable
frontmatter IDs.

Runs on the Mac as a stdio server. No network exposure.

---

## What it does

Seven tools and two resource entry points, registered with any MCP host
(Claude Code, Claude Desktop, Cline, etc.):

| Resource | Type | What it returns |
|---|---|---|
| `vault://quick-index` | resource | The Quick Index navigation hub (`report-playbook-mcp-index`) |
| `vault://doc/{doc_id}` | resource template | One stable vault doc rendered as markdown, with the same sensitivity policy as `read_doc` |

| Tool | Read or write | What it answers |
|---|---|---|
| `find_runbook(query, limit=5)` | read | "How do I add an NPM proxy host?" |
| `lookup_system(name)` | read | "Tell me about AdGuard Home" |
| `read_doc(doc_id)` | read | Returns the markdown body for `public`, `internal`, and `sensitive` docs. `secret_adjacent` requires explicit request plus local unlock. |
| `inventory_for_project(slug)` | read | "What docs are part of homelab-dns?" |
| `recent_changes(days=7)` | read | Recent vault commits within indexed folders |
| `add_frontmatter(...)` | write | Prepends a fully-validated frontmatter block to a vault doc that doesn't have one. Refuses if frontmatter already exists. |
| `update_frontmatter(...)` | write | Updates fields on a doc that already has frontmatter. `doc_id` is immutable; everything else is fair game. `touch_last_reviewed=True` is the common "I just re-read this runbook" pattern. |

All seven respect the schema documented at
`02 Infrastructure/Severino HQ/Frontmatter Schema.md` in the vault.

Resources are for stable readable knowledge objects. Tools are for search,
lookup, mutation, and workflow-style actions.

The `vault://quick-index` resource exposes `report-playbook-mcp-index`, the
Severino Labs navigation hub. The `vault://doc/{doc_id}` resource template lets
MCP clients read a known doc directly as markdown once a stable `doc_id` is
known. Both paths use the same sensitivity policy as `read_doc`.

---

## Documentation

| Doc | Purpose |
|---|---|
| `QUICKSTART.md` | Command-first setup guide for sample-vault and real-vault adoption. |
| `STRUCTURE.md` | File-by-file repository map. |
| `docs/demo.md` | Short transcript of the intended MCP assistant flow. |
| `docs/testing-ci.md` | Local test commands, CI matrix, and test coverage notes. |
| `docs/ai-safety-security.md` | AI safety model, sensitivity gate, local unlock, audit logging, and threat assumptions. |
| `.github/SECURITY.md` | GitHub vulnerability reporting policy. |

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
searched as metadata and pointed at, but their bodies are withheld unless the
caller requests access and the local Mac authorizes a one-request unlock.

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

### Reproducible demo

This repo includes a fake Obsidian-style vault under `examples/sample-vault/`.
It has the same folder layout and frontmatter contract as the private vault,
but contains only safe sample docs.

Run the test suite against the project:

```bash
uv run pytest
```

Start the MCP against the sample vault:

```bash
SKR_VAULT_PATH=examples/sample-vault uv run severino-knowledge-router
```

In an MCP client, the intended AI workflow is:

| User intent | First MCP action | Second MCP action |
|---|---|---|
| Broad question: "How do I expose a service over HTTPS?" | Read `vault://quick-index` | Read `vault://doc/{doc_id}` for the target runbook |
| Specific question: "What's the cert generation runbook?" | `find_runbook("cert generation")` | Read `vault://doc/{doc_id}` or call `read_doc` on the top hit |
| System question: "Tell me about AdGuard Home" | `lookup_system("AdGuard Home")` | Read `vault://doc/{doc_id}` for the relevant doc |
| Secret-adjacent question: "Show me the offline CA doc" | `read_doc("infra-offline-ca")` | `read_doc(..., include_secret_adjacent=True)` only when explicitly needed; local unlock still required |

Expected demo behavior:

- `vault://quick-index` returns the sample navigation hub.
- `vault://doc/rb-generate-homelab-cert` returns the sample certificate runbook.
- `find_runbook("generate homelab certificate")` returns `rb-generate-homelab-cert`.
- `vault://doc/infra-offline-ca` and `read_doc("infra-offline-ca")` withhold
  the body by default because the doc is marked `secret_adjacent`.
- `read_doc("infra-offline-ca", include_secret_adjacent=True)` still withholds
  the body unless local interactive unlock is enabled and succeeds.

See `docs/demo.md` for a short transcript of the intended assistant flow.

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
    -- uv run --directory $HOME/Documents/Code/Assets/severino-knowledge-router severino-knowledge-router
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

Restart the Claude Desktop app. The 7 tools and 2 resource entry points should
appear in the MCP picker.

---

## Configuration (env vars)

| Var | Default | Purpose |
|---|---|---|
| `SKR_VAULT_PATH` | `~/Documents/Code/Severino Labs` | Vault root |
| `SKR_INDEXED_DIRS` | `01 Projects:02 Infrastructure:03 Runbooks` | Colon-separated subdirs the loader recurses into |
| `SKR_HQ_URL` | `https://hq.jseverino.com` | Reserved for the future HQ integration |
| `SKR_CACHE_SECONDS` | `30` | How long the in-memory vault index stays warm |
| `SKR_ALLOW_SECRET_ADJACENT_UNLOCK` | `false` | Enables hidden local unlock prompts for `read_doc(..., include_secret_adjacent=True)` |
| `SKR_SECRET_ADJACENT_UNLOCK_HASH` | unset | Salted unlock hash, mainly for tests or temporary local use |
| `SKR_SECRET_ADJACENT_UNLOCK_HASH_FILE` | `~/.config/severino-knowledge-router/secret-adjacent-unlock.sha256` | Local file containing the salted unlock hash |
| `SKR_SECRET_ADJACENT_UNLOCK_KEYCHAIN_SERVICE` | `severino-knowledge-router` | macOS Keychain service name for the salted unlock hash |
| `SKR_SECRET_ADJACENT_UNLOCK_KEYCHAIN_ACCOUNT` | `secret-adjacent-unlock` | macOS Keychain account name for the salted unlock hash |
| `SKR_SECRET_ADJACENT_UNLOCK_AUDIT_LOG` | `~/.local/state/severino-knowledge-router/audit.log` | Local audit log for unlock attempts; no body content is logged |

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
| `sensitive` | Full body + advisory |
| `secret_adjacent` | Metadata only + policy note by default; full body only after explicit request plus local unlock |

`secret_adjacent` is the marker for anything adjacent to credentials, CA
keys, plaintext rotation procedures, etc. By default, the tool tells the LLM
where the doc lives in Obsidian without returning the body.

To release one secret-adjacent body through the MCP, all conditions must pass:

- The caller requests `read_doc(..., include_secret_adjacent=True)`.
- `SKR_ALLOW_SECRET_ADJACENT_UNLOCK=1` is set in the local MCP environment.
- A salted unlock hash is configured in macOS Keychain, the hash file, or
  `SKR_SECRET_ADJACENT_UNLOCK_HASH`.
- The local hidden-input prompt on the Mac succeeds.

Do not type the unlock phrase into AI chat. The prompt is local-only, and the
unlock is valid for one `read_doc` request.

Recommended macOS setup stores the salted hash in Keychain, not the phrase:

```bash
HASH="$(python3 -c 'import getpass,hashlib,os; p=getpass.getpass("MCP unlock phrase: "); s=os.urandom(16); print(f"sha256:{s.hex()}:{hashlib.sha256(s + p.encode()).hexdigest()}")')"
security add-generic-password -U \
  -s severino-knowledge-router \
  -a secret-adjacent-unlock \
  -w "$HASH"
```

Then enable prompts only in the MCP config where you want them:

```json
{
  "mcpServers": {
    "severino-knowledge-router": {
      "command": "severino-knowledge-router",
      "env": {
        "SKR_ALLOW_SECRET_ADJACENT_UNLOCK": "1"
      }
    }
  }
}
```

`search_body` never searches secret-adjacent bodies, even when its deprecated
compatibility flag is set. Use `read_doc` for per-doc local unlock.

---

## License

MIT — see `LICENSE`.

## Status

v0.2.1. Read tools, frontmatter write tools, Quick Index resource discovery,
and the reproducible sample vault are complete. HQ JSON integration (assets,
expenses, projects) is deferred until HQ has a proper API token.
