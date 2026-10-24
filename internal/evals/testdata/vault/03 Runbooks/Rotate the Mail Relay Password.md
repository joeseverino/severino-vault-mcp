---
doc_id: rb-rotate-mail-relay
title: Rotate the Mail Relay Password
doc_type: runbook
system: Mail Relay
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-07-25
related_projects: []
related_assets: []
tags: [mail, scanner]
---

# Rotate the Mail Relay Password

## Create the password

In the mail provider's admin page, reset the password of the `relay@example.com`
account and limit it to sending.

## Where it goes

Store it in the password manager item `Mail relay`, field `password`. The
relay container reads it as `SMTP_RELAY_PASSWORD` from its env file at
`/srv/relay/.env`; restart the relay after changing it.

## Verify

Scan one page to email from the copier and confirm it arrives.
