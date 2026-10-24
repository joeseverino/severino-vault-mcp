---
doc_id: rb-deliver-carrier-key
title: Deliver the Carrier API Key
doc_type: runbook
system: Shipping
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-09-20
related_projects: []
related_assets: []
tags: [shipping, labels]
---

# Deliver the Carrier API Key

## Goal

Give the label service on the office server the carrier's API key, so it can
buy postage and print shipping labels.

## Steps

1. Create a production key in the carrier's developer portal for the
   `label-service` app.
2. Copy it to the office server over SSH into `/srv/labels/.env` as
   `CARRIER_API_KEY`.
3. Restart the label service and print a test label from its health page.
