---
doc_id: rb-install-station-agent
title: Install the Station Agent
doc_type: deployment_guide
system: Quire
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-03
related_projects: [quire]
related_assets: []
tags: [quire, stations]
---

# Install the Station Agent

## Goal

Put the Quire station agent on a new press station so it reports press status
and pulls the station's print queue.

## Steps

1. Add the station to `deploy/stations.toml` with its VPN name.
2. Run `mise run station:install STATION=<name>`.
3. Confirm the station shows as online on the Quire press board.

## Upgrade

Run the install task again; it replaces the binary and restarts the service.
