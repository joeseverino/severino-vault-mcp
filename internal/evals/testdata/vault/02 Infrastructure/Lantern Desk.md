---
doc_id: infra-lantern-desk
title: Lantern Desk
doc_type: architecture_note
system: Lantern Desk
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-14
related_projects: [lantern-desk]
related_assets: []
tags: [homelab, lantern, lantern-desk, ops-ui]
---

# Lantern Desk

Lantern Desk is the lab's help desk: tickets, on-call notes and the status
page, served at https://desk.example.com.

## Architecture

One web container and one worker share a database on the app host. The
worker sends notifications and closes stale tickets.

## Deploy

A merge to `main` builds the image, and the deploy job on the app host pulls
it and restarts the containers. The steps are in the deploy runbook.

## Plugins

Ticket sources are plugins: mail, chat and the monitoring webhook.
