---
doc_id: infra-beacon
title: Beacon
doc_type: architecture_note
system: Beacon
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-10
related_projects: [beacon]
related_assets: []
tags: [beacon, ops-app, hub]
---

# Beacon

Beacon is the operations app for the lab: inventory, certificates, DNS
rewrites, proxy hosts and the agent API, served at https://beacon.example.com.

## Architecture

A Django app with a Postgres database on the app host, behind the reverse
proxy. Background jobs run in a worker container. The edge controller on the
VPS reports back to it over the tailnet.

## Development

Local development uses mise tasks; see Beacon Local Development for the dev
server, fixtures and test database.

## Deployment

Deploys run from CI on merge to main through the self-hosted runner. The edge
controller is installed separately on each edge target; see Install the Edge
Controller.

## Plugins

Extensions live in their own repos and register through the plugin manifest.
See Add a Beacon Plugin.

## MCP

Beacon exposes typed resources to agents. See Beacon MCP Architecture.
