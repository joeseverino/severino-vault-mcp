---
doc_id: rb-generate-internal-cert
title: Generate Internal Service Certificate
doc_type: runbook
system: Local PKI
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-05-17
related_projects:
  - client-edge-dns
related_assets: []
tags:
  - pki
  - certificate
  - tls
---

## Goal

Generate a signed TLS certificate for an internal service.

## Commands

```bash
cd ~/Documents/Code/Projects/cert-generator
./cert-gen <service>.internal.example
```

## Steps

1. Boot the offline CA VM.
2. Run `./cert-gen <service>.internal.example`.
3. Enter the CA key passphrase when prompted.
4. Confirm output files appear under the service certificate directory.
5. Update the certificate inventory.
