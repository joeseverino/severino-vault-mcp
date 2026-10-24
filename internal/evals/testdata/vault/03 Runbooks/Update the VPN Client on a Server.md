---
doc_id: rb-update-vpn-server
title: Update the VPN Client on a Server
doc_type: runbook
system: Office VPN
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-10-06
related_projects: []
related_assets: []
tags: [vpn, updates]
---

# Update the VPN Client on a Server

## Linux servers

```sh
sudo apt-get update && sudo apt-get install --only-upgrade meshvpn
meshctl version
```

## After the update

Run `meshctl status` and confirm the server is still in the servers group.
