---
doc_id: rb-generate-internal-cert
title: Generate an Internal Certificate
doc_type: runbook
system: Local PKI
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-10-03
related_projects: []
related_assets: []
tags: [pki, certificates]
---

# Generate an Internal Certificate

## Steps

```sh
cd ~/src/office-ca
mise run issue NAME=<name>.office.example.com
```

The last step asks for the CA key passphrase, so someone has to be at the
keyboard. The certificate and key are written to `out/<name>/`.

## Install

Copy both to the office server's Traefik certificate directory and reload
Traefik.
