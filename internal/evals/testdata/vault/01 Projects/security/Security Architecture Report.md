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

The controls that protect the shop's systems, grouped by area, with the risk
each one answers. The shop is six people, two press stations, an office server
and one storefront VPS, so every control here has to be one that a part-time
administrator can keep running.

## Identity

- **Security keys for every staff login.** Phishing is the likeliest way in for
  a small shop, and a hardware key cannot be typed into a fake login page.
- **An admin group for admin pages.** Quire, the wiki and Proof Room check the
  `shop-admins` claim, so a front counter login cannot change prices or users.
- **Eight-hour sessions.** A shift is eight hours; a login left open on the
  counter PC expires before the next morning.

## Network

- **Servers reachable only over the office VPN.** The office server has no
  port open to the internet, so a leaked password alone cannot reach it.
- **Public traffic only through the storefront VPS.** Customers reach the
  storefront and Proof Room there; the VPS forwards nothing into the office.
- **A default-deny VPN policy.** Press stations can reach Quire and the print
  queue and nothing else, which limits what a compromised station can touch.

## Secrets

- **Services read secrets through their own logins.** A scheduled job never
  borrows a person's vault session, so revoking a person never breaks a job.
- **No secret in a repository.** Env files are written at deploy time from the
  password manager and are never committed.
- **Rotation recorded beside the secret.** Each password manager item notes
  when it was last rotated and what reads it.

## Certificates

- **An offline root key.** The internal CA's root key stays in the office safe
  and is used only to sign the intermediate.
- **Automatic public renewals.** The storefront certificate renews through ACME
  on the VPS without anyone logging in.
- **Expiry alerts two weeks ahead.** The `#it` channel hears about a
  certificate that failed to renew long before customers would see an error.

## Backups

- **Nightly archives.** Every container volume and database dump is archived
  to the NAS each night.
- **A weekly offsite copy, encrypted.** A fire or theft at the shop does not
  take the backups with it.
- **A restore drill every quarter.** An archive nobody has restored is only a
  hope; the drill proves it can be read.

## Monitoring

- **Uptime checks on every public name.** The storefront and Proof Room are
  checked every minute from outside the shop.
- **Alerts to a chat channel.** Failures post to `#it`, where whoever is on
  call sees them on their phone.
- **A weekly look at open findings.** Monday's stand-up starts with anything
  the checks or the audit left open.
