---
doc_id: rb-beacon-local-development
title: Beacon Local Development
doc_type: runbook
system: Beacon
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-09-28
related_projects: [beacon]
related_assets: []
tags: [beacon, development]
---

# Beacon Local Development

## Prerequisites

mise, Docker Desktop and the repo checked out at ~/Code/beacon.

## Start the dev server

```sh
cd ~/Code/beacon
mise run db:up
mise run dev
```

The dev server listens on http://localhost:8000 and reloads on save.

## Fixtures

`mise run seed` loads the sample inventory. Reset with `mise run db:reset`.

## Tests

`mise run test` runs the fast suite against the dev database.
