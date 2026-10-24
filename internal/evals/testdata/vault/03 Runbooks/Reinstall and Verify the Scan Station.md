---
doc_id: rb-reinstall-scan-station
title: Reinstall and Verify the Scan Station
doc_type: runbook
system: Scan Station
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-18
related_projects: []
related_assets: []
tags: [scanner, office]
---

# Reinstall and Verify the Scan Station

## Steps

```sh
sudo apt-get install --reinstall scan-station
scan-station selftest
```

Scan one page to the shared drive and check that it lands in `Scans/Inbox`.
