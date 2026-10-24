---
doc_id: rb-restore-from-backup
title: Restore From Backup
doc_type: recovery_procedure
system: Backups
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-05-11
related_projects: []
related_assets: []
tags: [backup, borg, recovery]
---

# Restore From Backup

## Steps

1. List the archives with `borg list`.
2. Extract the volume to a scratch path and compare it with the live one.
3. Stop the service, swap the volume in, start it again.
