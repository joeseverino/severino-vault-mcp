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
  - homelab-dns
related_assets: []
tags:
  - dns
  - adguard
  - homelab
---

# AdGuard Home

AdGuard Home provides internal DNS filtering and local hostname resolution for
the sample homelab.

## Notes

- Local services resolve under `.homelab`.
- Nginx Proxy Manager terminates HTTPS for browser-facing services.
