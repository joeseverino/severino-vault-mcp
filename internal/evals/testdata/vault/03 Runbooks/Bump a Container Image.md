---
doc_id: rb-bump-container-image
title: Bump a Container Image
doc_type: runbook
system: Docker
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-15
related_projects: []
related_assets: []
tags: [docker, updates]
---

# Bump a Container Image

## Steps

1. Change the image tag in the service's `compose.yaml` to the new version.
2. Pull and recreate:

```sh
docker compose pull
docker compose up -d
```

3. Check the container logs and the health endpoint.

## Rollback

Set the previous tag back and run the same two commands.
