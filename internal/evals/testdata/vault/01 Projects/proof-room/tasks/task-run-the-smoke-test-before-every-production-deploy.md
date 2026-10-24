---
doc_id: task-run-the-smoke-test-before-every-production-deploy
title: Run the smoke test automatically before every production deploy
doc_type: task
status: open
related_projects:
  - proof-room
tags:
  - backlog
  - deploy
  - ci
  - testing
  - smoke-test
effort: M
priority: med
created: 2026-10-01
---

# Run the smoke test automatically before every production deploy

**Problem.** The smoke test uploads a sample PDF and approves it, which
catches a broken renderer, but only when someone remembers to run it.

**Fix.** Make the deploy job run it first and stop on a failure.
