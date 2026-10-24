---
doc_id: rb-publish-writeup
title: Publish a Writeup
doc_type: runbook
system: Site
environment: wordpress
status: active
sensitivity: internal
last_reviewed: 2026-10-02
related_projects: []
related_assets: []
tags: [site, writing]
---

# Publish a Writeup

## Gate

Run `mise run publish:check <slug>`. It fails on missing images, broken
links and drafts still marked private.

## Publish

Set `published: true` in the writeup's frontmatter, commit, and run the site
sync.
