---
doc_id: rb-deploy-proof-room
title: Deploy Proof Room
doc_type: deployment_guide
system: Proof Room
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-10-02
related_projects: [proof-room]
related_assets: []
tags: [proofs, release]
---

# Deploy Proof Room

## Goal

Ship a change to the customer proofing portal on the storefront VPS.

## Routine: landing on `main` is the deploy

1. Merge the pull request.
2. The build job pushes the image; the deploy job pulls it on the VPS and
   restarts the web container and the renderer.
3. Watch the deploy job until it is green.

## First-time setup

Create the database, copy the env file to `/srv/proofs`, then run the deploy
job once by hand.

## Verify

Open https://proofs.example.com/health and check the version it reports.

## Common Mistakes

- Treating a green test run as the deploy: only the deploy job going green is.
