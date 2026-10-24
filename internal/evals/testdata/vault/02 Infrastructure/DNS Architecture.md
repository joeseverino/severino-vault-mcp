---
doc_id: infra-dns
title: DNS Architecture
doc_type: architecture_note
system: AdGuard Home
environment: adguard
status: active
sensitivity: internal
last_reviewed: 2026-08-14
related_projects: []
related_assets: []
tags: [dns, adguard, cloudflare]
---

# DNS Architecture

## Internal resolution

AdGuard Home on the app host answers for the LAN and the tailnet. Internal
names resolve through DNS rewrites to the reverse proxy address 192.0.2.10.

## Public DNS

Public records live in Cloudflare. The zone example.com holds the site, the
VPS and the mail records. Certificate renewals use the DNS-01 challenge.

## Split horizon

Names under lab.example.com exist only as rewrites; they never appear in the
public zone.
