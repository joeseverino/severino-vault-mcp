---
doc_id: report-quick-index-navigation
title: Navigate by a Quick Index, Not Full-Text Search
doc_type: decision_record
system: Vault MCP
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-17
related_projects: []
related_assets: []
tags:
  - mcp
  - navigation
  - decision
---

# Navigate by a Quick Index, Not Full-Text Search

## Context

Broad questions ("how do I expose a service over HTTPS?") don't map cleanly to a
single keyword, and full-text search returns plausible-but-wrong docs.

## Decision

Keep one Quick Index (`report-playbook-mcp-index`) as the navigation hub. Broad
questions start there and route intent → `doc_id` → the specific doc.

## Consequences

- One place encodes "where do I look for X".
- The MCP reads the index first, then the target doc — fewer wrong answers.
- Decision records like this one live under `02 Infrastructure/00 Reporting/`.
