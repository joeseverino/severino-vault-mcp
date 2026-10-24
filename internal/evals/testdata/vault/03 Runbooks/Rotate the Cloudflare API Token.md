---
doc_id: rb-rotate-cloudflare-token
title: Rotate the Cloudflare API Token
doc_type: runbook
system: Cloudflare
environment: cloudflare
status: active
sensitivity: internal
last_reviewed: 2026-07-25
related_projects: []
related_assets: []
tags: [cloudflare, certificates]
---

# Rotate the Cloudflare API Token

## Create the token

In the Cloudflare dashboard, create a token with `Zone:DNS:Edit` on
example.com only.

## Where it goes

Store it in the Secret Store item `Cloudflare DNS`, field `token`. The
certificate renewer reads it as `CF_DNS_API_TOKEN` from its env file at
`/opt/renewer/.env`; restart the renewer after changing it.

## Verify

Run a dry-run renewal and confirm the TXT record appears and is removed.
