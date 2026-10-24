---
doc_id: rb-quire-local-development
title: Quire Local Development
doc_type: runbook
system: Quire
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-09-28
related_projects: [quire]
related_assets: []
tags: [quire, development]
---

# Quire Local Development

## Prerequisites

mise, Docker and the repo checked out at ~/src/quire.

## Start the dev server

```sh
cd ~/src/quire
mise run db:up
mise run dev
```

The dev server listens on http://localhost:8000 and reloads on save.

## Sample data

`mise run seed` loads sample orders and paper stock. Reset with `mise run db:reset`.

## Tests

`mise run test` runs the fast suite against the dev database.
