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

What the spring audit of the shop's systems found, and the fix chosen for
each finding. Open items are tracked in the backlog and reviewed at the first
staff meeting of each month.

## Secrets

The alertbot hooks for the press stations, the mail relay password and the
carrier API key for the label service all sat in plain env files on the office
server. Each now lives in the password manager, and the service reads it at
start. A security key prompt at the front counter during a scheduled job means
the job was reading the vault as a person instead of through its service login.

## Identity

The wiki and Quire shared one OIDC client, so revoking one app's access would
have locked staff out of both. Every app now gets its own client ID and
redirect URI in the identity provider, and each app's settings are checked
against the client the provider shows.

## Network

The front counter PC's VPN client was four months out of date because
auto-update was off, and the servers were only updated when someone noticed.
Turn on auto-update wherever the client offers it, and record each server
update in the IT changelog.

## Containers

Three stacks ran images tagged `latest`, so a restart could change versions
without anyone choosing to. Pin every image to a version; an update then means
bumping the pin, running docker compose pull and recreating the stack.

## Certificates

The internal CA key passphrase was typed while the screen was shared on a
video call. The storefront certificate was renewed with an account that could
also change the shop's DNS.

## Access

The file server's admin account used a password three people knew. It now has
a break-glass key held in the office safe, and every use is written in the
access log.

## Quire

The dev server ran with debug on against a copy of real customer orders; the
station agent install script downloaded its binary without checking a
checksum; extensions ran with the worker's full database rights.

## Backups

Nobody had run a restore drill in two quarters. The nightly archives were
complete, and pruning matched the written policy.

## Documentation

The runbooks for local DNS records, proxy routes and the journal publish check
were current. The security architecture report described a VPN layout the shop
replaced in the spring.
