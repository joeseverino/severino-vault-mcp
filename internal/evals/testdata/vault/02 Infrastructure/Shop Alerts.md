---
doc_id: infra-shop-alerts
title: Shop Alerts
doc_type: architecture_note
system: Alertbot
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-09-02
related_projects: []
related_assets: []
tags: [alerts, alertbot, chat]
---

# Shop Alerts

## Bot

Alertbot runs on the storefront VPS and posts into the shop's chat. Channels
are private, and every sender needs a hook that may post to its channel.

## Channels

- `#it`: monitoring and certificate expiry.
- `#press-floor`: press stations reporting a jam, low stock or a finished run.
- `#orders`: new orders and approved proofs.

## Hooks

Hooks are created with the alertbot admin CLI and kept in the password
manager.
