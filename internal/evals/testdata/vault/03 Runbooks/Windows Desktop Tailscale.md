---
doc_id: rb-windows-desktop-tailscale
title: Windows Desktop Tailscale
doc_type: runbook
system: Tailscale
environment: tailscale
status: active
sensitivity: internal
last_reviewed: 2026-08-20
related_projects: []
related_assets: []
tags: [tailscale, windows]
---

# Windows Desktop Tailscale

## Install

Install the Windows client from the vendor's MSI and sign in with the admin
account.

## Auto-update

Turn on automatic updates so the client keeps itself current:

```powershell
tailscale set --auto-update
```

Confirm with `tailscale debug prefs` that `AutoUpdate.Apply` is true.

## Run unattended

Enable unattended mode so the tailnet stays up when no one is logged in.
