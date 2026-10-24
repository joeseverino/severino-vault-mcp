---
doc_id: rb-update-tailscale-server
title: Update Tailscale on a Server
doc_type: runbook
system: Tailscale
environment: tailscale
status: active
sensitivity: internal
last_reviewed: 2026-10-06
related_projects: []
related_assets: []
tags: [tailscale, updates]
---

# Update Tailscale on a Server

## Linux servers

```sh
sudo apt-get update && sudo apt-get install --only-upgrade tailscale
tailscale version
```

## After the update

Check `tailscale status` and confirm the node still carries its tags.
