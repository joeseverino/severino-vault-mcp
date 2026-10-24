---
doc_id: infra-quire-api
title: Quire API Architecture
doc_type: architecture_note
system: Quire
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-09-20
related_projects: [quire]
related_assets: []
tags: [quire, api]
---

# Quire API Architecture

## Transport

The API is served by the Quire web process under /api/v1 as JSON over HTTPS.
Each client authenticates with its own API key, issued from the admin page.

## Endpoints

`/jobs`, `/presses` and `/stock` list and fetch print jobs, press status and
paper stock. Every response is one typed record from the database; no endpoint
returns free text.

## Webhooks

Quire posts a signed webhook when a job changes state. Proof Room subscribes
to `job.proof_ready`; the station agent subscribes to `job.scheduled`.
