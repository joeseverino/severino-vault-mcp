---
doc_id: rb-add-quire-extension
title: Add a Quire Extension
doc_type: runbook
system: Quire
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-02
related_projects: [quire]
related_assets: []
tags: [quire, extensions]
---

# Add a Quire Extension

## Steps

1. Create the extension repo from the template.
2. Declare its pages and jobs in `extension.toml`.
3. Add it to the extension manifest in the Quire repo and run `mise run extensions:sync`.
4. Restart the worker so the extension's jobs register.
