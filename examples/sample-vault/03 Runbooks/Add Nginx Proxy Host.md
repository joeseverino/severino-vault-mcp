---
doc_id: rb-add-nginx-proxy-host
title: Add Nginx Proxy Host
doc_type: runbook
system: Nginx Proxy Manager
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-05-17
related_projects:
  - client-edge-dns
related_assets: []
tags:
  - nginx
  - proxy
  - https
---

## Goal

Expose an internal service over HTTPS through Nginx Proxy Manager.

## Steps

1. Open Nginx Proxy Manager.
2. Add a proxy host for `<service>.internal.example`.
3. Set the forward hostname and port for the service container.
4. Attach the matching certificate.
5. Save and verify `https://<service>.internal.example`.

## Expected Result

The service loads over HTTPS without a browser certificate warning.
