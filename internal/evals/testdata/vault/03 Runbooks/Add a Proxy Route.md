---
doc_id: rb-add-proxy-route
title: Add a Proxy Route
doc_type: runbook
system: Traefik
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-06-02
related_projects: []
related_assets: []
tags: [traefik, proxy]
---

# Add a Proxy Route

## Goal

Expose an internal service over HTTPS.

## Steps

1. Add a local DNS record for the name.
2. Add Traefik router labels to the service's container, pointing at its port.
3. Set the router's TLS to the internal certificate and redirect HTTP to HTTPS.
