---
doc_id: rb-install-edge-controller
title: Install the Edge Controller
doc_type: deployment_guide
system: Beacon
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-10-03
related_projects: [beacon]
related_assets: []
tags: [beacon, edge]
---

# Install the Edge Controller

## Goal

Put the Beacon edge controller on a new edge target so it reports health and
applies proxy routes.

## Steps

1. Add the target to `deploy/targets.toml` with its tailnet name.
2. Run `mise run edge:install TARGET=<name>`.
3. Confirm the target shows as healthy on the Beacon topology page.

## Upgrade

Re-run the install task; it replaces the binary and restarts the unit.
