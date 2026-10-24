---
doc_id: infra-identity-provider
title: Identity Provider
doc_type: architecture_note
system: Identity Provider
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-06-05
related_projects: []
related_assets: []
tags: [sso, oidc, identity]
---

# Identity Provider

## Overview

The identity provider signs staff in to every web app at the shop with OIDC
and accepts only security keys; there are no passwords to phish.

## Clients

Each app is a registered OIDC client with its own redirect URI. See Add an
OIDC Client for the procedure.

## Groups

Group claims gate admin pages: `shop-admins` and `shop-staff`.
