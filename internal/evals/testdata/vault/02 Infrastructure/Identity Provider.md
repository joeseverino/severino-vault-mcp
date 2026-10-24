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

The identity provider issues OIDC tokens for every web app in the lab and
holds passkeys only; there are no passwords.

## Clients

Each app is a registered OIDC client with its own redirect URI. See Add an
OIDC Client for the procedure.

## Groups

Group claims gate admin pages: `lab-admins` and `lab-users`.
