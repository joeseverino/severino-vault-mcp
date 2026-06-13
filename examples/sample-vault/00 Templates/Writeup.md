---
title: CHANGEME
description: >-
  Short card and SEO summary. One or two sentences. Keep this in frontmatter
  only — never repeat it as the first paragraph of the body.
published: false                  # flip to true when the writeup is ready to ship
published_at: {{date:YYYY-MM-DD}}
last_reviewed: {{date:YYYY-MM-DD}}
cover_image: ./images/cover.png
cover_alt: >-
  CHANGEME — describe what the cover image actually shows; the card and
  hero fall back to the title without it
technologies:
  - CHANGEME                      # slugs from 06 Pages/_technology-groups.md
featured: false                   # set true + featured_order to surface on the home page
# featured_order: 1
related_projects: []              # vault-only — stripped on site sync
related_assets: []                # vault-only — stripped on site sync
---

# {{title}}

<!-- Opening paragraph: the problem this writeup solves or the system it
documents. Avoid restating the description above. -->

## Background

...

## What I built

...

## How it works

...

## Lessons learned

...

<!--
Authoring patterns
==================

Private-link tooltip:
  For links to private services (Tailscale-only dashboards, internal
  tools), use a standard markdown link with a `"private: "` title
  prefix. On click, a small floating tooltip shows the message instead
  of navigating, so public visitors see why the link exists without
  hitting a connection error.

      [Severino HQ](https://hq.jseverino.com "private: this site only works on my tailnet")

  The `"private: "` prefix is the marker. `src/lib/content.ts` rewrites
  any matching link to `<a ... data-private-tooltip="message">` during
  markdown render. The click handler lives in
  `src/components/PrivateTooltip.astro` and is auto-included by any page
  whose rendered body contains `data-private-tooltip` — no manual wiring,
  and pages without private links never load the script. Styling lives
  under "Private-link tooltip" in `src/styles/base.css`.
-->

