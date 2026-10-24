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

The internal CA signs certificates for office.example.com names. The root key
lives on an encrypted USB drive in the office safe and comes out only for a
signing session. The intermediate signs every leaf certificate and is
replaced each January.
