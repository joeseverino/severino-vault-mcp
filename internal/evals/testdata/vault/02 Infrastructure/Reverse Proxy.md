---
doc_id: infra-reverse-proxy
title: Reverse Proxy
doc_type: architecture_note
system: Nginx Proxy Manager
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-06-02
related_projects: []
related_assets: []
tags: [nginx, proxy, tls]
---

# Reverse Proxy

## Layout

Nginx Proxy Manager on the app host terminates TLS for every internal
service. Each service gets a proxy host with the internal certificate.

## Certificates

Internal names use certificates from the local CA. Public names use
certificates renewed through the DNS-01 challenge.
