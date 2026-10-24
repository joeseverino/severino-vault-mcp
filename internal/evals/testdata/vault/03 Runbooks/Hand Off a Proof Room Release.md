---
doc_id: rb-hand-off-proof-room-release
title: Hand Off a Proof Room Release
doc_type: runbook
system: Proof Room
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-10-16
related_projects: [proof-room]
related_assets: []
tags: [proof-room, release, process, deploy, lessons]
---

# Hand Off a Proof Room Release

## Goal

Hand a Proof Room release from the developer to whoever watches the deploy.

## Procedure

### 1. Write the release note

List what changed for customers and for the press room.

### 2. Check the proofs

Approve one sample proof on the staging copy.

### 3. Merge

Merge the pull request during shop hours, never at closing.

### 4. Deploy

Once it merges, watch the deploy job go green and approve a real proof.

## Lessons

Add anything that went wrong to the lessons note before the next release.
