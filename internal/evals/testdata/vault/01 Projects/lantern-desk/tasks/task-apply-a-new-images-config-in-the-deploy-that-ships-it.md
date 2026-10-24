---
doc_id: task-apply-a-new-images-config-in-the-deploy-that-ships-it
title: Apply a new image's config in the deploy that ships it
doc_type: task
status: open
related_projects:
  - lantern-desk
tags:
  - deploy
  - containers
effort: S
priority: high
created: 2026-09-23
---

# Apply a new image's config in the deploy that ships it

**Problem.** The deploy restarts the containers with the previous image's
config file and copies the new one only after the health check, so a config
change takes effect one deploy late.

**Fix.** Copy the config out of the verified image before the restart, and
keep the prior copy for rollback.
