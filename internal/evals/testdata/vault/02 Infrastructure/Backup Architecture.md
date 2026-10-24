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
tags: [backup, borg]
---

# Backup Architecture

## What is backed up

Container volumes, the Postgres dumps and the shared drive. Borg writes a
nightly archive to the NAS; a weekly copy goes offsite, encrypted.

## Retention

Borg prunes to seven daily, four weekly and twelve monthly archives.
