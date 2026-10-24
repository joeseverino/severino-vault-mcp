---
doc_id: task-load-new-proof-templates-in-the-deploy-that-ships-them
title: Load new proof templates in the deploy that ships them
doc_type: task
status: open
related_projects:
  - proof-room
tags:
  - deploy
  - templates
effort: S
priority: high
created: 2026-09-23
---

# Load new proof templates in the deploy that ships them

**Problem.** The deploy restarts the portal with the previous release's proof
templates and copies the new ones only after the health check, so a template
change shows up one deploy late.

**Fix.** Copy the templates out of the verified image before the restart, and
keep the prior set for rollback.
