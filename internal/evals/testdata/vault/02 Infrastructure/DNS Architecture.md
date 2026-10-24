---
doc_id: infra-dns
title: DNS Architecture
doc_type: architecture_note
system: Pi-hole
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-08-14
related_projects: []
related_assets: []
tags: [dns, pihole, registrar]
---

# DNS Architecture

## Internal resolution

Pi-hole on the office server answers for the office network and the VPN.
Internal names resolve through local DNS records to the reverse proxy at
192.0.2.10.

## Public DNS

Public records live at the domain registrar. The zone example.com holds the
storefront, Proof Room and the mail records.

## Split horizon

Names under office.example.com exist only as local records; they never appear
in the public zone.
