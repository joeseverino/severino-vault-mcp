"""MCP server registration.

Most tools read the vault from disk. Two are vault writers (add_frontmatter,
update_frontmatter) that mutate vault `.md` files in place -- both validate
against the schema and refuse unsafe operations. doc_id is immutable across
all writes. A small jseverino.com operations group wraps fixed Wrangler/curl
workflows for the operator's own site; it is deliberately not a generic shell.

The Quick Index is also exposed as an MCP resource so clients can discover
the vault's navigation hub without spending a search-tool call.
"""

from __future__ import annotations

from mcp.server.fastmcp import FastMCP

from .config import Config
from .context import ServerContext
from .core_tools import register_core
from .tools import infra_datasets as infra_datasets_tools
from .tools import site_ops as site_ops_tools
from .tools import topology as topology_tools
from .tools import writeups as writeups_tools

QUICK_INDEX_DOC_ID = "report-playbook-mcp-index"
QUICK_INDEX_RESOURCE_URI = "vault://quick-index"
DOC_RESOURCE_TEMPLATE_URI = "vault://doc/{doc_id}"


_CTX = ServerContext(Config.from_env())
# config + loader back the generic core tools still inlined here (find / read /
# search / tasks / daily). They collapse into _CTX when the core moves to the
# engine; the loader is always needed, so building it now costs nothing.
_CONFIG = _CTX.config
_LOADER = _CTX.loader


_SERVER_INSTRUCTIONS = """\
This MCP routes the calling AI session to the right runbook, infrastructure
doc, or project metadata in the configured Obsidian-style operational vault.
The vault is expected to contain stable frontmatter IDs, a Quick Index, and
runbooks or infrastructure notes for the operator's environment.

OPERATING RULES (read this BEFORE answering any operational question):

1. For broad or cross-cutting questions ("how do I X", "where do I look",
   "what's the process for..."), START with the Quick Index navigation hub:
   read the `vault://quick-index` resource if your MCP host supports
   resources, or call `read_doc('report-playbook-mcp-index')` if it does not.
   Then read the specific target doc before answering. Prefer
   `vault://doc/{doc_id}` for stable doc reads when your MCP host supports
   resource templates; otherwise call `read_doc(doc_id)`.

2. If the user asks "what's the runbook for Y" or anything more specific
   resembling operational knowledge from the configured vault, you MUST call
   `find_runbook` (or `lookup_system`, or `search_body`) BEFORE generating
   any prose. Then call `read_doc` on the top hit and answer in the doc's
   own words — quote commands verbatim.

3. Do NOT generate a generic tutorial from training data when a runbook
   exists in the vault. That is the single failure mode this MCP is built
   to prevent. If the user's answer is "run cert-gen <host>", that is the
   entire answer — no openssl tutorial, no preamble.

4. If `find_runbook` returns no relevant hits, say so explicitly and offer
   to create the missing runbook (Write the file, then `add_frontmatter` to
   register it). Only fall back to general guidance when you've confirmed
   no doc exists, and label it "no doc exists for this — here's a general
   approach."

5. Match the doc's terseness in your reply. If the runbook is four lines,
   your answer is four lines. Long answers when the doc is short are a
   smell that you didn't actually read it.

6. For personal progress/log questions ("what progress did I make on Friday?",
   "what happened yesterday?"), use `daily_progress` first. Daily notes are
   intentionally outside the runbook index and live under `00 Inbox/Daily Note`.

SENSITIVITY GATE (don't be afraid of it):

- public / internal / sensitive — `read_doc` returns the full body. Bodies
  marked `sensitive` come with an `advisory` field; pass it along to the
  user but use the content.
- restricted — `read_doc` withholds the body by default. Pass
  `include_restricted=True` only when the user explicitly needs it.
  The local MCP still requires `SVMC_ALLOW_RESTRICTED_UNLOCK=1` plus a
  successful hidden local unlock prompt before releasing the body. These are
  docs about CA keys, credential rotation, the offline CA, etc.

WRITE TOOLS:

`add_frontmatter` and `update_frontmatter` mutate vault files. Use them
when the user asks to tag a doc, bump `last_reviewed`, deprecate something,
etc. After successful writes, remind the user to run any downstream vault
sync or indexing job their workflow requires.

JSEVERINO.COM WRITEUP WORKFLOW (MANDATORY for portfolio work):

When working on writeups in `05 Writeups/` or the technology catalog at
`06 Pages/_technology-groups.md`, you MUST use the dedicated tools below
instead of grepping or reading the files by hand. The manual workflow is
how publishes ship with wrong `featured_order` values, missing tech-catalog
slugs, or dangling image references; these tools exist to prevent that.

IMPORTANT FOR LOCAL MODELS:
- Do not print fake tool-call text such as `list_writeups("published")`.
- Actually invoke the MCP tool, wait for the JSON result, then answer from it.
- For any "what is the order / currently published / featured / home order"
  question, call `list_featured_writeup_order()` first (fast path).

Each tool's own description is authoritative; pick by intent, not by grep:

- READ state — `list_featured_writeup_order` (compact order), `list_writeups`
  (full inventory; `filter="featured"` is the only correct home-cloud order),
  `get_technology_catalog` (slugs/groups), `find_writeups_using_tag` (is a tag
  earned a featured slot?). Never `cat`/`rg` `05 Writeups/*/index.md` or hand-
  parse `_technology-groups.md` for these.
- VALIDATE — `validate_writeup` / `validate_all_writeups`. "I read the file" is
  not "I validated it." `writeup_dashboard` returns inventory + validation from
  one snapshot; use it instead of separate calls for the same screen.
- MUTATE (never `Edit` writeup YAML) — `update_writeup_frontmatter` for one
  writeup's scalars; `reorder_featured` / `apply_writeup_plan` for the featured
  list, which they keep sequential 1..N transactionally. Hand-shuffling
  `featured_order` across files is the exact failure mode that produced v2.4.2.

VERIFY BEFORE SHIPPING: call `prepare_writeup_publish(slug)` (or at minimum
`validate_writeup(slug)` + `list_writeups("featured")`) *immediately before*
the commit/push of a writeup, not after. It catches wrong `featured_order`,
missing tech-catalog entries, missing images, and unresolved `related_projects`
/ `related_assets` before they reach production. Shipping first and verifying
second is exactly how this MCP came to exist; do not repeat that pattern. Apply
the same discipline as Rule 2 above about runbooks.

The configured vault root and indexed directories are controlled by
`config.example.toml`-style settings or `SVMC_*` environment overrides.
"""

mcp = FastMCP("severino-vault-mcp", instructions=_SERVER_INSTRUCTIONS)


register_core(mcp, _CTX)


# ----- composed Labs tool groups --------------------------------------------
site_ops_tools.register(mcp, _CTX)
writeups_tools.register(mcp, _CTX)
topology_tools.register(mcp, _CTX)
infra_datasets_tools.register(mcp, _CTX)


# ----- entry point ------------------------------------------------------------

def run() -> None:
    """Start the MCP server over stdio. Invoked by `__main__.py` and the console script."""
    mcp.run()


__all__ = ["mcp", "run"]
