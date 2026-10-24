---
doc_id: rb-publish-journal-post
title: Publish a Journal Post
doc_type: runbook
system: Journal
environment: other
status: active
sensitivity: internal
last_reviewed: 2026-10-02
related_projects: []
related_assets: []
tags: [journal, writing]
---

# Publish a Journal Post

## Check

Run `mise run journal:check <slug>`. It fails on missing images, broken
links and posts still marked as drafts.

## Publish

Set `published: true` in the post's frontmatter, commit, and run the journal
sync.
