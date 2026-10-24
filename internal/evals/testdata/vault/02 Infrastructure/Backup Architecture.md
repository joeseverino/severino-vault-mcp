---
doc_id: infra-backups
title: Backup Architecture
doc_type: architecture_note
system: Backups
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-05-11
related_projects: []
related_assets: []
tags: [backup, restic]
---

# Backup Architecture

## What is backed up

Docker volumes, the Postgres dumps and the vault repo. Nightly snapshots go
to the NAS; a weekly copy goes offsite, encrypted.

## Retention

Seven daily, four weekly and six monthly snapshots.
