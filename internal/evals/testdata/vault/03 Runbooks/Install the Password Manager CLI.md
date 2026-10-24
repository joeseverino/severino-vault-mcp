---
doc_id: rb-install-password-cli
title: Install the Password Manager CLI
doc_type: runbook
system: Password Manager
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-07-19
related_projects: []
related_assets: []
tags: [passwords, cli]
---

# Install the Password Manager CLI

## Steps

```sh
brew install password-manager-cli
pwm --version
```

Sign in once with `pwm login`; after that the CLI unlocks with the security
key.
