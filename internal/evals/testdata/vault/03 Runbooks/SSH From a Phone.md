---
doc_id: rb-ssh-from-phone
title: SSH From a Phone
doc_type: runbook
system: SSH
environment: tailscale
status: active
sensitivity: internal
last_reviewed: 2026-09-21
related_projects: []
related_assets: []
tags: [ssh, mobile]
---

# SSH From a Phone

## Setup

Generate a key in the phone's SSH client, add the public key to the
servers' authorized keys, and connect over the tailnet name.
