---
doc_id: rb-agent-push-token
title: Give an Agent a Push Token
doc_type: runbook
system: Pushbox
environment: vps
status: active
sensitivity: internal
last_reviewed: 2026-10-18
related_projects: []
related_assets: []
tags: [notifications, agents]
---

# Give an Agent a Push Token

## Create the token

```sh
pushbox token add --user agents --label coding-agent
pushbox access agents agents write-only
```

## Store it

Put the token in the Secret Store item `Pushbox agents`, field `token`. The
notify helper reads it from there and publishes to the `agents` topic.
