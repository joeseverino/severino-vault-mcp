---
doc_id: infra-beacon-mcp
title: Beacon MCP Architecture
doc_type: architecture_note
system: Beacon
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-09-20
related_projects: [beacon]
related_assets: []
tags: [beacon, mcp, agents]
---

# Beacon MCP Architecture

## Transport

The MCP server runs inside the Beacon web process and speaks streamable HTTP
at /mcp. Clients authenticate with an OAuth client credential minted per
agent.

## Resources

`describe_resources`, `list_resource` and `get_resource` serve hosts,
containers, certificates and DNS rewrites. Every response is a typed record
from the registry; nothing is free text.

## Scopes

Read scopes are granted per client. Write capabilities require an explicit
grant and are audited.
