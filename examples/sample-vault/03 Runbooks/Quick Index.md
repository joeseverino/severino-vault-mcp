---
doc_id: report-playbook-mcp-index
title: Example Operations Vault Quick Index
doc_type: public_article_draft
system: Vault MCP
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-17
related_projects: []
related_assets: []
tags:
  - index
  - mcp
  - navigation
---

# Example Operations Vault Quick Index

Use this as the navigation hub for broad operational questions. Start here,
then read the specific runbook or infrastructure note before answering.

| Intent | Start Here | Then Read |
|---|---|---|
| Add HTTPS to an internal service | `rb-add-nginx-proxy-host` | `infra-adguard-home` |
| Generate or renew an internal certificate | `rb-generate-internal-cert` | `infra-offline-ca` only if explicitly needed |
| Understand local DNS | `infra-adguard-home` | `project-client-edge-dns` |

## AI Workflow

1. Broad question: read `vault://quick-index`.
2. Specific runbook question: call `find_runbook`.
3. Read `vault://doc/{doc_id}` for the chosen target doc before answering.
4. Use `read_doc` when you need structured metadata or an explicit
   `restricted` override.
