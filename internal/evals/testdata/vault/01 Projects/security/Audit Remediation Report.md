---
doc_id: report-audit-remediation
title: Audit Remediation Report
doc_type: decision_record
system: Security
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-09-18
related_projects: []
related_assets: []
tags: [security, audit, report]
---

# Audit Remediation Report

## Summary

Findings from the lab audit and the fix for each.

## Secrets

Agent tokens for the push server, the Cloudflare DNS token for the renewer and the connect server token were all stored in env files. Move each to the Secret Store and have the agent wrapper read it; a Touch ID prompt during an agent run means the wrapper was bypassed.

Status: tracked in the backlog; owner reviews secrets monthly. Status: tracked in the backlog; owner reviews secrets monthly. Status: tracked in the backlog; owner reviews secrets monthly. Status: tracked in the backlog; owner reviews secrets monthly. Status: tracked in the backlog; owner reviews secrets monthly. Status: tracked in the backlog; owner reviews secrets monthly.

## Identity

Two apps used a shared OIDC client. Give every app its own client ID and redirect URI in the identity provider, and check the client ID in each app's settings matches.

Status: tracked in the backlog; owner reviews identity monthly. Status: tracked in the backlog; owner reviews identity monthly. Status: tracked in the backlog; owner reviews identity monthly. Status: tracked in the backlog; owner reviews identity monthly. Status: tracked in the backlog; owner reviews identity monthly. Status: tracked in the backlog; owner reviews identity monthly.

## Network

The Windows desktop's Tailscale client was months behind because auto-update was off; Linux servers were updated by hand. Turn on auto-update where the client supports it and record server updates in the changelog.

Status: tracked in the backlog; owner reviews network monthly. Status: tracked in the backlog; owner reviews network monthly. Status: tracked in the backlog; owner reviews network monthly. Status: tracked in the backlog; owner reviews network monthly. Status: tracked in the backlog; owner reviews network monthly. Status: tracked in the backlog; owner reviews network monthly.

## Containers

Image versions were unpinned on three stacks. Pin every image, then bump the pin, run docker compose pull and recreate the stack when updating.

Status: tracked in the backlog; owner reviews containers monthly. Status: tracked in the backlog; owner reviews containers monthly. Status: tracked in the backlog; owner reviews containers monthly. Status: tracked in the backlog; owner reviews containers monthly. Status: tracked in the backlog; owner reviews containers monthly. Status: tracked in the backlog; owner reviews containers monthly.

## Certificates

The internal CA key passphrase was typed on a shared screen. The public certificate renewal used a token with account-wide scope.

Status: tracked in the backlog; owner reviews certificates monthly. Status: tracked in the backlog; owner reviews certificates monthly. Status: tracked in the backlog; owner reviews certificates monthly. Status: tracked in the backlog; owner reviews certificates monthly. Status: tracked in the backlog; owner reviews certificates monthly. Status: tracked in the backlog; owner reviews certificates monthly.

## Access

Root on the app host was reachable with a shared key. Replace it with the break-glass key and log every use.

Status: tracked in the backlog; owner reviews access monthly. Status: tracked in the backlog; owner reviews access monthly. Status: tracked in the backlog; owner reviews access monthly. Status: tracked in the backlog; owner reviews access monthly. Status: tracked in the backlog; owner reviews access monthly. Status: tracked in the backlog; owner reviews access monthly.

## Beacon

The dev server ran with debug on against a copy of production data; the edge controller install script fetched the binary without a checksum; plugins ran with the worker's full permissions.

Status: tracked in the backlog; owner reviews beacon monthly. Status: tracked in the backlog; owner reviews beacon monthly. Status: tracked in the backlog; owner reviews beacon monthly. Status: tracked in the backlog; owner reviews beacon monthly. Status: tracked in the backlog; owner reviews beacon monthly. Status: tracked in the backlog; owner reviews beacon monthly.

## Backups

The restore test had not run in two quarters. Snapshots were fine; retention matched the policy.

Status: tracked in the backlog; owner reviews backups monthly. Status: tracked in the backlog; owner reviews backups monthly. Status: tracked in the backlog; owner reviews backups monthly. Status: tracked in the backlog; owner reviews backups monthly. Status: tracked in the backlog; owner reviews backups monthly. Status: tracked in the backlog; owner reviews backups monthly.

## Documentation

Runbooks for the DNS rewrite, the proxy host and the writeup publish gate were current; the security architecture report was out of date.

Status: tracked in the backlog; owner reviews documentation monthly. Status: tracked in the backlog; owner reviews documentation monthly. Status: tracked in the backlog; owner reviews documentation monthly. Status: tracked in the backlog; owner reviews documentation monthly. Status: tracked in the backlog; owner reviews documentation monthly. Status: tracked in the backlog; owner reviews documentation monthly.

