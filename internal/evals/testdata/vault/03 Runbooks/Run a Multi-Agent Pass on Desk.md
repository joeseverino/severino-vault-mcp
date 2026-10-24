---
doc_id: rb-run-a-multi-agent-pass-on-desk
title: Run a Multi-Agent Pass on Desk
doc_type: runbook
system: lantern-desk
environment: local_mac
status: active
sensitivity: internal
last_reviewed: 2026-10-16
related_projects: [lantern-desk]
related_assets: []
tags: [lantern-desk, agents, process, deploy, lessons]
---

# Run a Multi-Agent Pass on Desk

## Goal

Land several pieces of help desk work in one pass, each built by its own agent.

## Procedure

### 1. Map the pieces

Give each piece its own files, so no two agents edit the same one.

### 2. Launch the workers

One worker per piece, each in its own worktree.

### 3. Integrate

Rebase each worker's branch onto the integration branch and fast-forward.

### 4. Deploy

Open one pull request; once it merges, watch the deploy job go green.

## Lessons

Write each worker's lessons into the lessons log before the next pass.
