---
doc_id: rb-renew-public-cert
title: Renew a Public Certificate
doc_type: runbook
system: Certificates
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-08-30
related_projects: []
related_assets: []
tags: [certificates, tls]
---

# Renew a Public Certificate

## Automatic renewal

The ACME client on the storefront VPS renews the storefront and Proof Room
certificates with the HTTP-01 challenge.

## Manual renewal

```sh
docker compose -f /srv/acme/compose.yaml run --rm acme renew --force
```

## Expiry alerts

Expiry warnings go to the `#it` channel 14 days ahead.
