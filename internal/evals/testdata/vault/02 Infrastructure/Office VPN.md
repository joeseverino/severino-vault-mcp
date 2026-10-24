---
doc_id: infra-vpn
title: Office VPN
doc_type: architecture_note
system: Office VPN
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-07-30
related_projects: []
related_assets: []
tags: [vpn, network]
---

# Office VPN

## Members

The office server, the storefront VPS, both press stations, the front counter
PC and the staff laptops are members. Phones join through the mobile client.

## Access policy

The policy file is kept in git and applied with `meshctl policy apply`. Groups
separate servers, stations and staff devices; servers accept SSH only from the
admin group.

## Names

The VPN's own name service is off; Pi-hole answers internal names instead.
