---
doc_id: infra-adguard-home
title: AdGuard Home
doc_type: architecture_note
system: AdGuard Home
environment: adguard
status: active
sensitivity: internal
last_reviewed: 2026-05-17
related_projects:
  - client-edge-dns
related_assets: []
tags:
  - dns
  - adguard
  - network-operations
---

# AdGuard Home

AdGuard Home provides internal DNS filtering and local hostname resolution for
the sample client edge environment.

## Notes

- Local services resolve under `.internal.example`.
- Nginx Proxy Manager terminates HTTPS for browser-facing services.
