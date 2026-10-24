---
doc_id: infra-reverse-proxy
title: Reverse Proxy
doc_type: architecture_note
system: Traefik
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-06-02
related_projects: []
related_assets: []
tags: [traefik, proxy, tls]
---

# Reverse Proxy

## Layout

Traefik on the office server terminates TLS for every internal service. Each
service gets a router defined by labels on its container.

## Certificates

Internal names use certificates from the local CA. The storefront and Proof
Room use public certificates renewed on the VPS.
