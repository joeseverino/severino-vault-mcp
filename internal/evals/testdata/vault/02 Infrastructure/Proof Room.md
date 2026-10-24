---
doc_id: infra-proof-room
title: Proof Room
doc_type: architecture_note
system: Proof Room
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-10-14
related_projects: [proof-room]
related_assets: []
tags: [proofs, proof-room, customers]
---

# Proof Room

Proof Room is where customers review and approve proofs before a job goes to
press, served at https://proofs.example.com.

## Architecture

One web container and one renderer share a database on the storefront VPS.
The renderer turns uploaded PDFs into page previews and marks up bleed and
trim lines.

## Deploy

A merge to `main` builds the image, and the deploy job on the VPS pulls it and
restarts the containers. The steps are in the deploy runbook.

## Integrations

Quire tells Proof Room when a proof is ready, and Proof Room tells Quire when
the customer approves it.
