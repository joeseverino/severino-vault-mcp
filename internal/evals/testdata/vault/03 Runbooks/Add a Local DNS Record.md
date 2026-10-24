---
doc_id: rb-add-local-dns-record
title: Add a Local DNS Record
doc_type: runbook
system: Pi-hole
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-09-12
related_projects: []
related_assets: []
tags: [dns, pihole]
---

# Add a Local DNS Record

## Steps

1. Open Pi-hole, Local DNS, DNS Records.
2. Add `<name>.office.example.com` pointing at 192.0.2.10.
3. Confirm with `dig <name>.office.example.com @192.0.2.53`.
