---
title: A Year of the Lab
published: true
date: 2026-10-01
---

# A Year of the Lab

## The tailnet

Every server, the VPS and the Windows desktop joined the tailnet. Keeping Tailscale updated on Linux is one apt command; on Windows the client can update itself once auto-update is set.

## DNS and certificates

Internal names are AdGuard rewrites to the proxy; public names live in Cloudflare and renew through DNS-01 with an API token. Internal certificates come from a local CA whose key needs a passphrase.

## Identity

One identity provider for every app, passkeys only. Each app is an OIDC client with its own redirect URI and client ID.

## The ops app

Beacon grew from an inventory page into the lab's control plane: a dev server in one command, an edge controller on the VPS, plugins, and an MCP server for agents.

## Containers

Every service is a compose stack with a pinned image. Updating means bumping the pin, compose pull, and up.

## Agents

Agents read secrets through a service account and publish to a push topic with their own token. The vault MCP answers their how-do-I questions.

## Backups

Nightly snapshots, a weekly offsite copy, and a restore test every quarter.
