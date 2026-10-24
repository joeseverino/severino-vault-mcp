---
doc_id: infra-push-notifications
title: Push Notifications
doc_type: architecture_note
system: Pushbox
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-09-02
related_projects: []
related_assets: []
tags: [notifications, pushbox]
---

# Push Notifications

## Server

Pushbox runs on the VPS at https://push.example.com. Topics are private;
every publisher needs a token with write access to its topic.

## Topics

- `alerts`: monitoring and certificate expiry.
- `agents`: agent runs that finish or need a decision.
- `deploys`: CI deploy results.

## Tokens

Tokens are created with the Pushbox admin CLI and stored in the Secret Store.
