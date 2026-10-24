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
cd ~/Code/cert-tool
mise run issue NAME=<name>.lab.example.com
```

The last step asks for the CA key passphrase, so it needs a person at the
keyboard. The certificate and key land in `out/<name>/`.

## Install

Upload both to the proxy host's certificate in Nginx Proxy Manager.
