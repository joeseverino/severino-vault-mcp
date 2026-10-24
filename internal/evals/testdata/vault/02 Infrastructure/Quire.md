---
doc_id: infra-quire
title: Quire
doc_type: architecture_note
system: Quire
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-10
related_projects: [quire]
related_assets: []
tags: [quire, job-tracker, hub]
---

# Quire

Quire is the shop's job tracker: orders, the press schedule, paper stock and
the API the other apps read, served at https://quire.example.com.

## Architecture

A Python web app with a Postgres database on the office server, behind the
reverse proxy. Background jobs run in a worker container. The station agent on
each press station reports back to it over the office VPN.

## Development

Local development runs through mise tasks; see Quire Local Development for the
dev server, sample data and the dev database.

## Deployment

A merge to main deploys from CI. The station agent is installed separately on
each press station; see Install the Station Agent.

## Extensions

Extensions live in their own repos and register through the extension
manifest. See Add a Quire Extension.

## API

Quire serves jobs, presses and stock to the other apps. See Quire API
Architecture.
