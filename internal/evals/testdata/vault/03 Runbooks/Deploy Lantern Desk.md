---
doc_id: rb-deploy-lantern-desk
title: Deploy Lantern Desk
doc_type: deployment_guide
system: Lantern Desk
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-02
related_projects: [lantern-desk]
related_assets: []
tags: [homelab, lantern]
---

# Deploy Lantern Desk

## Goal

Ship a change to the help desk on the app host.

## Routine: landing on `main` is the deploy

1. Merge the pull request.
2. The build job pushes the image; the deploy job pulls it on the app host
   and restarts the web container and the worker.
3. Watch the deploy job until it is green.

## First-time bringup

Create the database, copy the env file to `/opt/apps/desk`, then run the
deploy job once by hand.

## Verify

Open https://desk.example.com/health and check the version it reports.

## Common Mistakes

- Calling a green CI run a deploy: only the deploy job going green is.
