---
doc_id: rb-add-proxy-host
title: Add a Proxy Host
doc_type: runbook
system: Nginx Proxy Manager
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-06-02
related_projects: []
related_assets: []
tags: [nginx, proxy]
---

# Add a Proxy Host

## Goal

Expose an internal service over HTTPS.

## Steps

1. Add a DNS rewrite for the name.
2. In Nginx Proxy Manager, add a proxy host forwarding to the container.
3. Attach the internal certificate and force SSL.
