---
title: Internal HTTPS with a Private Certificate Authority
description: >-
  How an offline root CA issues TLS certificates for internal services, so
  homelab dashboards get real HTTPS without exposing anything to the public
  internet.
published: true
published_at: 2026-05-10
last_reviewed: 2026-05-17
cover_image: ./images/internal-https-cover.png
cover_alt: A browser address bar showing a valid padlock for dashboard.internal.example, served over HTTPS by a certificate from a private offline root CA.
technologies:
  - private-root-ca
  - nginx-proxy-manager
  - openssl
  - tailscale
featured: false
related_projects:
  - client-edge-dns
---

# Internal HTTPS with a Private Certificate Authority

> Example writeup. Every host and command is a placeholder; nothing here maps to
> real infrastructure. Binary assets (the cover image) are omitted from the
> sample vault.

Internal services deserve real TLS, not click-through certificate warnings. The
trick is to run your own certificate authority — kept offline — and have it sign
short-lived certificates for `*.internal.example` hosts that only your tailnet
can reach.

## The shape

1. A root CA lives on an offline VM and never touches the network.
2. A small script (`./cert-gen <service>.internal.example`) builds the CSR,
   signs it against the CA, and cleans up the key material.
3. Nginx Proxy Manager attaches the certificate to the service's proxy host.

The operational steps are captured as runbooks (`rb-generate-internal-cert`,
`rb-add-nginx-proxy-host`) so this writeup can stay about the *why*, while the
vault keeps the *how* one click away.

## Why this lives in the vault

This page is markdown in `05 Writeups/<slug>/index.md`. The same file Obsidian
renders is what the static site publishes — the vault is the CMS. Writeups carry
a different frontmatter shape from operational docs (`published`, `technologies`,
`featured`, …) because they feed a publishing pipeline, not the runbook index.
