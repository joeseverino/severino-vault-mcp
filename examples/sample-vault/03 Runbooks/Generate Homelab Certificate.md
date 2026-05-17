---
doc_id: rb-generate-homelab-cert
title: Generate Homelab Certificate
doc_type: runbook
system: Local PKI
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-05-17
related_projects:
  - homelab-dns
related_assets: []
tags:
  - pki
  - certificate
  - tls
---

## Goal

Generate a signed TLS certificate for an internal homelab service.

## Commands

```bash
cd ~/Documents/Code/Projects/cert-generator
./cert-gen <service>.homelab
```

## Steps

1. Boot the offline CA VM.
2. Run `./cert-gen <service>.homelab`.
3. Enter the CA key passphrase when prompted.
4. Confirm output files appear under the service certificate directory.
5. Update the certificate inventory.
