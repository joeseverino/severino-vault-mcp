---
doc_id: infra-agent-secret-access
title: Agent Secret Access
doc_type: architecture_note
system: Secret Store
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-09-21
related_projects: []
related_assets: []
tags: [secrets, agents, security]
---

# Agent Secret Access

## How agents read secrets

Agents never hold a personal session. They read through a service account
whose token sits in the Secret Store environment, scoped to the vaults the
work needs.

## The Touch ID prompt

A Touch ID prompt during an agent run means the command fell through to the
desktop app integration instead of the service account. Run it through the
agent wrapper; a subagent must never trigger the prompt.

## Vaults

Personal vaults are never granted to the service account.
