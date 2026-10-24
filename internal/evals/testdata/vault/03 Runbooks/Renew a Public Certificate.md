---
doc_id: rb-renew-public-cert
title: Renew a Public Certificate
doc_type: runbook
system: Certificates
environment: cloudflare
status: active
sensitivity: internal
last_reviewed: 2026-08-30
related_projects: []
related_assets: []
tags: [certificates, tls]
---

# Renew a Public Certificate

## Automatic renewal

The renewer container renews public certificates with the DNS-01 challenge
against Cloudflare, using the API token from its env file.

## Manual renewal

```sh
docker compose -f /opt/renewer/compose.yaml run --rm renewer renew --force
```

## Expiry alerts

Expiry warnings go to the `alerts` push topic 14 days ahead.
