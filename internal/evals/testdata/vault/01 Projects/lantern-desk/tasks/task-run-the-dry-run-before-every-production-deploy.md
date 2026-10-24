---
doc_id: task-run-the-dry-run-before-every-production-deploy
title: Run the dry run automatically before every production deploy
doc_type: task
status: open
related_projects:
  - lantern-desk
tags:
  - backlog
  - deploy
  - ci
  - testing
  - dry-run
effort: M
priority: med
created: 2026-10-01
---

# Run the dry run automatically before every production deploy

**Problem.** The dry run catches a broken migration, but only when someone
remembers to run it.

**Fix.** Make the deploy job run it first and stop on a failure.
