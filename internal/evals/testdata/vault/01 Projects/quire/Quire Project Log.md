---
doc_id: project-quire-log
title: Quire Project Log
doc_type: decision_record
system: Quire
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-15
related_projects: [quire]
related_assets: []
tags: [quire, log]
---

# Quire Project Log

## 2026-07-02

Started the station agent for the press room. The install task takes a station name; deploying to the stations still means copying the binary over by hand.

## 2026-07-09

Dev server: every local task now lives in mise. `mise run dev` replaces the old start script, and the dev environment needs the dev database up first.

## 2026-07-16

Drafted the extension manifest. An extension declares its pages and jobs in extension.toml, and the worker picks up the jobs when it restarts.

## 2026-07-23

The VPN client on the office server was two releases behind; updated it and pinned the package source. The front counter PC still needs auto-update switched on.

## 2026-07-30

Rotated the mail relay password after the scanner's scan-to-email started bouncing. The relay account can now only send from the shop's domain.

## 2026-08-06

Staff now sign in to Quire through the identity provider. The client ID in Quire's settings did not match the one the provider issued, and every login failed with invalid_client.

## 2026-08-13

Press stations can post to the press-floor channel. Each station gets its own alertbot hook, kept in the password manager.

## 2026-08-20

Bumped the Postgres image pin, ran docker compose pull and recreated the stack. The new version needed no migration.

## 2026-08-27

The label service on the office server lost its carrier API key after the restore drill; delivered the key again over SSH.

## 2026-09-03

Internal certificates: the issue task stops for the CA key passphrase at the end, so nobody can schedule it.

## 2026-09-10

API: endpoints for jobs, presses and paper stock, each returning one typed record. The front counter stopped keeping its own stock spreadsheet.

## 2026-09-17

The station agent now deploys from CI to every station listed in stations.toml.

## 2026-09-24

Backups: restore drill of the Quire volume passed; the archive schedule and pruning stay as they are.

## 2026-10-01

Onboarding review: the new developer read the hub when they needed the dev server runbook. Linked the runbooks from the hub.

## 2026-10-08

Port 8000 clashes with the proofing preview on the shop laptop; wrote down the override.

## 2026-10-15

Every container image bump follows one runbook now: change the tag, compose pull, up -d, then check health.
