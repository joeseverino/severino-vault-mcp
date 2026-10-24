---
doc_id: rb-counter-pc-vpn
title: Front Counter PC VPN
doc_type: runbook
system: Office VPN
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-08-20
related_projects: []
related_assets: []
tags: [vpn, windows]
---

# Front Counter PC VPN

## Install

Install the Windows client from the vendor's MSI and sign in with the shop's
admin account.

## Auto-update

Turn on automatic updates so the client keeps itself current:

```powershell
meshctl set --auto-update
```

Confirm with `meshctl prefs` that `AutoUpdate` is true.

## Run as a service

Set the client to run as a service so the VPN stays up while nobody is
signed in.
