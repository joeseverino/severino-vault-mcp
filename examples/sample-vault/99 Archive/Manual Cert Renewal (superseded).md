---
doc_id: rb-manual-cert-renewal
title: Manual Cert Renewal (superseded)
doc_type: runbook
system: Local PKI
environment: local_mac
status: archived
sensitivity: internal
last_reviewed: 2026-02-01
related_projects: []
related_assets: []
tags:
  - pki
  - archived
---

# Manual Cert Renewal (superseded)

Kept for history. Superseded by `rb-generate-internal-cert`, which scripts the
whole flow. Archived docs stay in `99 Archive/` and out of the indexed dirs, so
they don't surface in day-to-day search but the trail isn't lost.

## Old procedure

1. Hand-build a CSR with `openssl`.
2. Copy it to the CA host.
3. Sign, copy back, install — every step manual and easy to get wrong.
