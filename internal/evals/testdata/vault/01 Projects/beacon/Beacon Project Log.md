---
doc_id: project-beacon-log
title: Beacon Project Log
doc_type: decision_record
system: Beacon
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-15
related_projects: [beacon]
related_assets: []
tags: [beacon, log]
---

# Beacon Project Log

## 2026-07-02

Started the edge controller. The install task takes a target name; the deploy pipeline still copies the binary by hand.

## 2026-07-09

Dev server: moved every local task into mise. `mise run dev` replaces the old shell script; the dev environment needs the test database up first.

## 2026-07-16

Plugin manifest drafted. A plugin declares resources in plugin.toml and the worker registers its jobs on restart.

## 2026-07-23

Tailscale on the VPS was two versions behind; updated it and pinned the apt source. The Windows desktop still needs auto-update turned on.

## 2026-07-30

Rotated the Cloudflare token after the renewer logged a 403. The new token only has DNS edit on the zone.

## 2026-08-06

Wired OIDC login through the identity provider. The client ID in settings did not match the provider's and every login failed with invalid_client.

## 2026-08-13

Agents can now publish to the push topic. Each agent gets its own token; the notify helper reads it from the Secret Store.

## 2026-08-20

Bumped the Postgres image pin, ran docker compose pull and recreated the stack. No migration needed.

## 2026-08-27

The connect server on the app host lost its token after the restore test; delivered a new one over SSH.

## 2026-09-03

Internal certificates: the issue task asks for the CA key passphrase at the end, so it cannot run unattended.

## 2026-09-10

MCP: typed resources for hosts, certificates and DNS rewrites. Agents stopped grepping the inventory.

## 2026-09-17

Edge controller deploy now runs from CI against each target in targets.toml.

## 2026-09-24

Backups: restore test of the Beacon volume passed; snapshots and retention unchanged.

## 2026-10-01

Efficiency review: agents reading the hub when they wanted the dev server runbook. Linked the runbooks from the hub.

## 2026-10-08

Port 8000 conflicts with another dev server on the laptop; documented the override.

## 2026-10-15

Container image bumps go through one runbook now: change the tag, compose pull, up -d, check health.
