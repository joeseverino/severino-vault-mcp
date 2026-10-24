---
doc_id: infra-tailnet
title: Tailnet Overview
doc_type: architecture_note
system: Tailscale
environment: tailscale
status: active
sensitivity: internal
last_reviewed: 2026-07-30
related_projects: []
related_assets: []
tags: [tailscale, network]
---

# Tailnet Overview

## Members

The app host, the VPS, the Windows desktop and the laptop are tailnet
members. Phones join through the mobile app.

## Access policy

The ACL policy file is kept in git and applied with the admin API. Tags
separate servers from personal devices; servers accept SSH only from tagged
admin devices.

## MagicDNS

MagicDNS is off; AdGuard answers internal names instead.
