---
doc_id: rb-add-beacon-plugin
title: Add a Beacon Plugin
doc_type: runbook
system: Beacon
environment: homelab
status: active
sensitivity: internal
last_reviewed: 2026-10-02
related_projects: [beacon]
related_assets: []
tags: [beacon, plugins]
---

# Add a Beacon Plugin

## Steps

1. Create the plugin repo from the template.
2. Declare its resources in `plugin.toml`.
3. Add it to the plugin manifest in the Beacon repo and run `mise run plugins:sync`.
4. Restart the worker so the plugin's jobs register.
