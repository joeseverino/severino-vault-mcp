---
doc_id: infra-local-pki
title: Local PKI
doc_type: architecture_note
system: Local PKI
environment: local_mac
status: active
sensitivity: restricted
last_reviewed: 2026-04-01
related_projects: []
related_assets: []
tags: [pki, ca, certificates]
---

# Local PKI

The internal CA signs certificates for lab.example.com names. The CA private
key lives offline on encrypted media and is mounted only for a signing
session. The intermediate signs leaf certificates and is rotated yearly.
