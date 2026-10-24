---
doc_id: infra-shared-passwords
title: Shared Password Vault
doc_type: architecture_note
system: Password Manager
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-09-21
related_projects: []
related_assets: []
tags: [passwords, staff, security]
---

# Shared Password Vault

## How staff reach shared logins

Shared logins (the carrier portal, the paper supplier, the registrar) live in
the `Shop` vault of the password manager. Staff open them with their own
account; nobody shares a master password.

## The security key prompt

The password manager asks for a tap on the security key when a vault opens on
a new device. A prompt during a scheduled job means the job was set up with a
person's account; move it to a service login.

## Service logins

Each service that reads a secret has its own login, limited to the one vault
it needs. Personal vaults are never shared with a service login.
