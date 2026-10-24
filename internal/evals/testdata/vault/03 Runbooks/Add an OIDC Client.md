---
doc_id: rb-add-oidc-client
title: Add an OIDC Client
doc_type: runbook
system: Identity Provider
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-06-05
related_projects: []
related_assets: []
tags: [oidc, sso]
---

# Add an OIDC Client

## Steps

1. In the identity provider's admin page, create a client named after the app.
2. Set the redirect URI to `https://<app>.office.example.com/auth/callback`,
   with no trailing slash.
3. Copy the client ID and client secret into the app's environment as
   `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET`.
4. Match the client ID in the app's settings to the one the provider shows;
   a mismatch fails with `invalid_client`.

## Groups

Allow the `shop-staff` group, and `shop-admins` for admin pages.
