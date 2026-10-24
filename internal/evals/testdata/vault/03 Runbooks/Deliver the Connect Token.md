---
doc_id: rb-deliver-connect-token
title: Deliver the Connect Token
doc_type: runbook
system: Secret Store
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-09-20
related_projects: []
related_assets: []
tags: [secrets, connect]
---

# Deliver the Connect Token

## Goal

Get the secrets connect server on the app host its access token.

## Steps

1. Create the token in the Secret Store admin console for the `connect`
   server.
2. Copy it to the app host over SSH into `/opt/connect/.env` as
   `CONNECT_TOKEN`.
3. Restart the connect container and check its health endpoint.
