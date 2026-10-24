---
doc_id: note-nginx-proxy-manager-primer
title: Nginx Proxy Manager primer
doc_type: architecture_note
system: Nginx Proxy Manager
environment: other
status: active
sensitivity: public
last_reviewed: 2026-05-15
related_projects: []
related_assets: []
tags:
  - nginx
  - proxy
  - tls
---

# Nginx Proxy Manager primer

## What it is

A web UI over Nginx for reverse-proxying internal services and attaching TLS
certificates. In this example setup it fronts the internal dashboards and serves
certificates issued by the offline root CA.

## Mental model

One proxy host per service: a hostname (`<service>.internal.example`), a forward
target (container host + port), and a certificate. The actual steps live in the
runbook `rb-add-nginx-proxy-host`.

## Notes

- Reference notes are background reading, not procedures. They are indexed and
  validated like the operational docs.
