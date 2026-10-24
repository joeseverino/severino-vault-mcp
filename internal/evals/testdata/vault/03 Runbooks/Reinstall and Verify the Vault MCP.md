---
doc_id: rb-reinstall-vault-mcp
title: Reinstall and Verify the Vault MCP
doc_type: runbook
system: Vault MCP
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-10-18
related_projects: []
related_assets: []
tags: [mcp, vault]
---

# Reinstall and Verify the Vault MCP

## Steps

```sh
go install ./cmd/vault-mcp
vault-mcp doctor
```

Compare `vault-mcp --fingerprint` against the source tree's.
