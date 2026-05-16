# Security policy

`severino-knowledge-router` runs as a local MCP server on Joe's Mac. It has no
network listening surface — it speaks stdio to its parent process (Claude Code,
Claude Desktop, etc.) and reads files the local user account can already read.

## Reporting a vulnerability

Email `security@<this-domain>` (replace with the operator's address) or use
GitHub private vulnerability reporting on this repo. **Do not** open a public
issue.

## Threat model

In scope:

- Bypass of the sensitivity gate (e.g. `secret_adjacent` body returned to an LLM).
- Path-traversal in the write tools (writing outside the vault).
- Bypass of frontmatter validation (writing invalid enum values, bad `doc_id` prefixes).
- Schema-version mismatch between the MCP and the vault.

Out of scope:

- Anything that requires write access to the user's home directory the user already has.
- LLM hallucinations about doc bodies. The MCP returns truthful data; the
  consuming LLM is responsible for not misrepresenting it.
- Compromised MCP host. If your Claude Code/Desktop install is malicious, the
  MCP is the least of your problems.
