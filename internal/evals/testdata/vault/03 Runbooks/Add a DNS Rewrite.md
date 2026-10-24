---
doc_id: rb-add-dns-rewrite
title: Add a DNS Rewrite
doc_type: runbook
system: AdGuard Home
environment: adguard
status: active
sensitivity: internal
last_reviewed: 2026-09-12
related_projects: []
related_assets: []
tags: [dns, adguard]
---

# Add a DNS Rewrite

## Steps

1. Open AdGuard Home, Filters, DNS rewrites.
2. Add `<name>.lab.example.com` pointing at 192.0.2.10.
3. Confirm with `dig <name>.lab.example.com @192.0.2.53`.
