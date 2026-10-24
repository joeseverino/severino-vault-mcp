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
tags: [backup, restic, recovery]
---

# Restore From Backup

## Steps

1. List snapshots with `restic snapshots`.
2. Restore the volume to a scratch path and compare.
3. Stop the service, swap the volume, start it again.
