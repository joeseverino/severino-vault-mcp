---
title: A First Year of Shop IT
published: true
date: 2026-10-01
---

# A First Year of Shop IT

## The VPN

The office server, the VPS, both press stations and the front counter PC joined the office VPN. Updating the client on Linux is one apt command; on Windows the client can update itself once auto-update is set.

## DNS and certificates

Internal names are Pi-hole records pointing at the proxy; public names live at the registrar and renew over ACME on the VPS. Internal certificates come from a local CA whose key needs a passphrase.

## Identity

One identity provider for every app, security keys only. Each app is an OIDC client with its own redirect URI and client ID.

## The job tracker

Quire grew from an order list into the shop's control board: a dev server in one command, a station agent on every press, extensions, and an API the other apps read.

## Containers

Every service is a compose stack with a pinned image. Updating means bumping the pin, compose pull, and up.

## Alerts

Press stations post to the press-floor channel through their own alertbot hooks, and the monitoring checks post to the IT channel.

## Backups

Nightly archives, a weekly offsite copy, and a restore drill every quarter.
