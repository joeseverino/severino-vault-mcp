---
doc_id: rb-station-alert-hook
title: Give a Press Station an Alert Hook
doc_type: runbook
system: Alertbot
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-10-18
related_projects: []
related_assets: []
tags: [alerts, stations]
---

# Give a Press Station an Alert Hook

## Create the hook

```sh
alertbot hook add --channel press-floor --label station-2
alertbot grant press-floor station-2 post-only
```

## Store it

Put the hook URL in the password manager item `Alertbot press-floor`, field
`hook`. The station agent reads it from there and posts to the `#press-floor`
channel.
