"""Canonical frontmatter schema — the profile framework plus the Labs profile.

A :class:`SchemaProfile` is the engine-generic unit: a named bundle of the enum
sets and id prefixes that decide what counts as a valid indexed doc, with the
logic to emit itself as JSON and to check a human schema doc against itself. The
framework carries no domain knowledge — a second vault (Education) registers its
own profile with different doc-types and statuses; only the values differ.

``LABS_PROFILE`` is the canonical Severino Labs ops profile. The MCP validates
writes against it, and Severino HQ consumes ``LABS_PROFILE.as_dict()`` (emitted by
``severino-vault-mcp schema --json``, committed to HQ and checked against this
module) so the two systems cannot drift on what ``hq sync`` will accept.

The module-level names (``DOC_TYPES``, ``as_dict``, ``check_doc_enums`` …) are the
Labs profile's fields and methods, kept as backward-compatible aliases so existing
imports keep working as the engine is extracted. Edit the Labs values here and
nowhere else.
"""

from __future__ import annotations

import re
from dataclasses import dataclass


@dataclass(frozen=True)
class SchemaProfile:
    """One vault's frontmatter contract: its enum sets, prefixes, and task lifecycle.

    Tasks are a second schema profile *within* a vault, not just another doc_type:
    a task carries its own lifecycle vocabulary (a runbook can never be "parked")
    and a slimmer required-field set. The write path branches on doc_type to
    validate against the right one; everything else (the lenient index, search, the
    graph) treats a task like any other doc.
    """

    name: str
    doc_types: frozenset[str]
    environments: frozenset[str]
    statuses: frozenset[str]
    sensitivities: frozenset[str]
    doc_id_prefixes: tuple[str, ...]
    required_fields: tuple[str, ...]
    task_statuses: frozenset[str]
    task_required_fields: tuple[str, ...]
    # The fields the task write path manages beyond the shared ones: a task closes
    # the loop with `closed:` (kept, not deleted) and associates to its project(s)
    # through the relation registry (related_projects), not a parallel id.
    task_fields: tuple[str, ...]

    def as_dict(self) -> dict[str, list[str]]:
        """Return the profile as a deterministic, JSON-serializable dict.

        Set-valued fields are sorted so the emitted JSON is byte-stable across
        runs; ordered fields (prefixes, required fields) keep their declared
        order. For the Labs profile this is the exact shape HQ commits and
        validates against.
        """
        return {
            "doc_types": sorted(self.doc_types),
            "environments": sorted(self.environments),
            "statuses": sorted(self.statuses),
            "sensitivities": sorted(self.sensitivities),
            "doc_id_prefixes": list(self.doc_id_prefixes),
            "required_fields": list(self.required_fields),
            "task_statuses": sorted(self.task_statuses),
            "task_required_fields": list(self.task_required_fields),
        }

    def _enum_fields(self) -> dict[str, frozenset[str]]:
        return {
            "doc_type": self.doc_types,
            "environment": self.environments,
            "status": self.statuses,
            "sensitivity": self.sensitivities,
        }

    def check_doc_enums(self, text: str) -> list[str]:
        """Compare a human schema doc's enum lines to this profile's sets.

        A schema doc documents each enum as a pipe-separated line
        (``doc_type: a | b | c``). This finds the first such list per field and
        reports any divergence from the canonical sets, so the human doc cannot
        silently drift from the profile. Returns human-readable mismatches; empty
        means the doc is current.
        """
        pattern = re.compile(
            r"^\s*(doc_type|environment|status|sensitivity)\s*:\s*(.+?)\s*$"
        )
        found: dict[str, set[str]] = {}
        for line in text.splitlines():
            match = pattern.match(line)
            if not match:
                continue
            field, rhs = match.group(1), match.group(2)
            rhs = rhs.split("#", 1)[0]  # strip trailing comment
            tokens = {token.strip() for token in rhs.split("|") if token.strip()}
            if len(tokens) <= 1:
                continue  # a single example value, not the enum list
            # The schema doc also documents the task lifecycle on its own `status:`
            # line; it is validated as the task profile, not the standard one, so
            # skip it here regardless of where it appears (order-independent).
            if field == "status" and tokens == set(self.task_statuses):
                continue
            found.setdefault(field, tokens)

        mismatches: list[str] = []
        for field, canonical in self._enum_fields().items():
            documented = found.get(field)
            if documented is None:
                mismatches.append(f"{field}: no enum list found in the doc")
                continue
            if documented != set(canonical):
                parts = []
                if extra := sorted(documented - set(canonical)):
                    parts.append(f"doc lists unknown {extra}")
                if missing := sorted(set(canonical) - documented):
                    parts.append(f"doc is missing {missing}")
                mismatches.append(f"{field}: " + "; ".join(parts))
        return mismatches


# The canonical Severino Labs ops profile. Edit these values, nowhere else.
LABS_PROFILE = SchemaProfile(
    name="labs",
    doc_types=frozenset({
        "runbook", "architecture_note", "deployment_guide",
        "troubleshooting_guide", "recovery_procedure",
        "public_article_draft", "decision_record",
        "task",
    }),
    environments=frozenset({
        "homelab", "vps", "wordpress", "cloudflare", "tailscale",
        "adguard", "unifi", "local_mac", "other",
    }),
    statuses=frozenset({"draft", "active", "deprecated", "archived"}),
    sensitivities=frozenset({"public", "internal", "sensitive", "restricted"}),
    doc_id_prefixes=("rb-", "infra-", "report-", "project-", "note-", "task-"),
    required_fields=(
        "doc_id", "title", "doc_type", "system", "environment", "status",
        "sensitivity",
    ),
    task_statuses=frozenset({"open", "active", "parked", "done", "wontfix"}),
    task_required_fields=("doc_id", "title", "doc_type", "status"),
    task_fields=(
        "status", "related_projects", "effort", "priority", "created", "closed",
    ),
)

# A second profile, proving the framework parameterizes a non-ops vault: the
# Georgia Tech / education vault. Courses, course notes, and assignments instead
# of runbooks; a course lifecycle instead of doc statuses; a lighter required-field
# set (no system / environment / sensitivity). The universal task lifecycle is
# reused verbatim — a task is a task in any vault.
EDUCATION_PROFILE = SchemaProfile(
    name="education",
    doc_types=frozenset({
        "course", "course_note", "assignment", "resource", "task",
    }),
    environments=frozenset({"gatech", "cert", "other"}),
    statuses=frozenset({
        "upcoming", "active", "completed", "dropped", "draft", "archived",
    }),
    sensitivities=frozenset({"public", "internal"}),
    doc_id_prefixes=("course-", "cnote-", "asg-", "res-", "task-"),
    required_fields=("doc_id", "title", "doc_type", "status"),
    task_statuses=frozenset({"open", "active", "parked", "done", "wontfix"}),
    task_required_fields=("doc_id", "title", "doc_type", "status"),
    task_fields=(
        "status", "related_projects", "effort", "priority", "created", "closed",
    ),
)


# Backward-compatible module-level names: the Labs profile is the default vault
# contract, so these aliases keep existing `from .schema import DOC_TYPES`-style
# imports working unchanged through the engine extraction.
DOC_TYPES = LABS_PROFILE.doc_types
ENVIRONMENTS = LABS_PROFILE.environments
STATUSES = LABS_PROFILE.statuses
SENSITIVITIES = LABS_PROFILE.sensitivities
DOC_ID_PREFIXES = LABS_PROFILE.doc_id_prefixes
REQUIRED_FIELDS = LABS_PROFILE.required_fields
TASK_STATUSES = LABS_PROFILE.task_statuses
TASK_REQUIRED_FIELDS = LABS_PROFILE.task_required_fields
TASK_FIELDS = LABS_PROFILE.task_fields


def as_dict() -> dict[str, list[str]]:
    """Emit the canonical Labs schema (the shape HQ commits and validates)."""
    return LABS_PROFILE.as_dict()


def check_doc_enums(text: str) -> list[str]:
    """Check a human schema doc against the canonical Labs profile."""
    return LABS_PROFILE.check_doc_enums(text)
