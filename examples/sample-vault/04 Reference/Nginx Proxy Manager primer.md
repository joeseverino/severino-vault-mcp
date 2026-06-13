---
type: reference
tags: [nginx, proxy, tls]
created: 2026-05-15
---

# Nginx Proxy Manager — primer

## What it is

A web UI over Nginx for reverse-proxying internal services and attaching TLS
certificates. In this example setup it fronts the internal dashboards and serves
certificates issued by the offline root CA.

## Mental model

One proxy host per service: a hostname (`<service>.internal.example`), a forward
target (container host + port), and a certificate. The actual steps live in the
runbook `rb-add-nginx-proxy-host`.

## Notes

- Reference notes use a lighter frontmatter (`type: reference`) and live outside
  the indexed operational dirs — background reading, not procedures.
