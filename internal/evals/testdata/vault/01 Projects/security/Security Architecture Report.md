---
doc_id: report-security-architecture
title: Security Architecture Report
doc_type: architecture_note
system: Security
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-09-20
related_projects: []
related_assets: []
tags: [security, architecture, report]
---

# Security Architecture Report

## Summary

The lab's security controls by area, with the threat each answers.

## Identity

- **Passkeys only on the identity provider.** This control covers identity for every host and is reviewed with the others. This control covers identity for every host and is reviewed with the others. This control covers identity for every host and is reviewed with the others. This control covers identity for every host and is reviewed with the others.
- **Admin group gates every admin page.** This control covers identity for every host and is reviewed with the others. This control covers identity for every host and is reviewed with the others. This control covers identity for every host and is reviewed with the others. This control covers identity for every host and is reviewed with the others.
- **Session lifetime eight hours.** This control covers identity for every host and is reviewed with the others. This control covers identity for every host and is reviewed with the others. This control covers identity for every host and is reviewed with the others. This control covers identity for every host and is reviewed with the others.

## Network

- **Servers reachable only over the tailnet.** This control covers network for every host and is reviewed with the others. This control covers network for every host and is reviewed with the others. This control covers network for every host and is reviewed with the others. This control covers network for every host and is reviewed with the others.
- **Public ingress only through the VPS.** This control covers network for every host and is reviewed with the others. This control covers network for every host and is reviewed with the others. This control covers network for every host and is reviewed with the others. This control covers network for every host and is reviewed with the others.
- **Default-deny ACL policy.** This control covers network for every host and is reviewed with the others. This control covers network for every host and is reviewed with the others. This control covers network for every host and is reviewed with the others. This control covers network for every host and is reviewed with the others.

## Secrets

- **Agents read through a scoped service account.** This control covers secrets for every host and is reviewed with the others. This control covers secrets for every host and is reviewed with the others. This control covers secrets for every host and is reviewed with the others. This control covers secrets for every host and is reviewed with the others.
- **No secret in a repo or env file under git.** This control covers secrets for every host and is reviewed with the others. This control covers secrets for every host and is reviewed with the others. This control covers secrets for every host and is reviewed with the others. This control covers secrets for every host and is reviewed with the others.
- **Rotation recorded in the Secret Store.** This control covers secrets for every host and is reviewed with the others. This control covers secrets for every host and is reviewed with the others. This control covers secrets for every host and is reviewed with the others. This control covers secrets for every host and is reviewed with the others.

## Certificates

- **Offline CA key.** This control covers certificates for every host and is reviewed with the others. This control covers certificates for every host and is reviewed with the others. This control covers certificates for every host and is reviewed with the others. This control covers certificates for every host and is reviewed with the others.
- **Public renewals through DNS-01.** This control covers certificates for every host and is reviewed with the others. This control covers certificates for every host and is reviewed with the others. This control covers certificates for every host and is reviewed with the others. This control covers certificates for every host and is reviewed with the others.
- **Expiry alerts 14 days ahead.** This control covers certificates for every host and is reviewed with the others. This control covers certificates for every host and is reviewed with the others. This control covers certificates for every host and is reviewed with the others. This control covers certificates for every host and is reviewed with the others.

## Backups

- **Nightly snapshots.** This control covers backups for every host and is reviewed with the others. This control covers backups for every host and is reviewed with the others. This control covers backups for every host and is reviewed with the others. This control covers backups for every host and is reviewed with the others.
- **Weekly encrypted offsite copy.** This control covers backups for every host and is reviewed with the others. This control covers backups for every host and is reviewed with the others. This control covers backups for every host and is reviewed with the others. This control covers backups for every host and is reviewed with the others.
- **Quarterly restore test.** This control covers backups for every host and is reviewed with the others. This control covers backups for every host and is reviewed with the others. This control covers backups for every host and is reviewed with the others. This control covers backups for every host and is reviewed with the others.

## Monitoring

- **Uptime checks on every public name.** This control covers monitoring for every host and is reviewed with the others. This control covers monitoring for every host and is reviewed with the others. This control covers monitoring for every host and is reviewed with the others. This control covers monitoring for every host and is reviewed with the others.
- **Alerts to the push topic.** This control covers monitoring for every host and is reviewed with the others. This control covers monitoring for every host and is reviewed with the others. This control covers monitoring for every host and is reviewed with the others. This control covers monitoring for every host and is reviewed with the others.
- **Weekly review of findings.** This control covers monitoring for every host and is reviewed with the others. This control covers monitoring for every host and is reviewed with the others. This control covers monitoring for every host and is reviewed with the others. This control covers monitoring for every host and is reviewed with the others.
