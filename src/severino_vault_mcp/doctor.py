"""Vault validation helpers for messy-vault onboarding."""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from pathlib import Path

from .config import Config
from .schema import (
    DOC_ID_PREFIXES,
    DOC_TYPES,
    ENVIRONMENTS,
    REQUIRED_FIELDS,
    SENSITIVITIES,
    STATUSES,
)
from .vault import _coerce_list, _split_frontmatter


@dataclass
class DoctorFinding:
    relative_path: str
    severity: str
    message: str
    proposal: str | None = None


@dataclass
class DoctorReport:
    vault_path: Path
    checked_files: int = 0
    indexed_docs: int = 0
    findings: list[DoctorFinding] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not any(f.severity == "error" for f in self.findings)

    def add(self, finding: DoctorFinding) -> None:
        self.findings.append(finding)


def validate_vault(config: Config, *, propose: bool = False) -> DoctorReport:
    report = DoctorReport(vault_path=config.vault_path)
    seen_doc_ids: dict[str, str] = {}
    for path in _iter_markdown_files(config):
        report.checked_files += 1
        relative_path = _relative(path, config.vault_path)
        try:
            text = path.read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            report.add(DoctorFinding(relative_path, "error", f"cannot read file: {exc}"))
            continue

        fm, _body, _body_start_line = _split_frontmatter(text)
        if not fm:
            report.add(DoctorFinding(
                relative_path,
                "error",
                "missing YAML frontmatter",
                _proposal_for(path, config.vault_path) if propose else None,
            ))
            continue

        doc_id = fm.get("doc_id")
        if doc_id:
            report.indexed_docs += 1
            doc_id = str(doc_id)
            if doc_id in seen_doc_ids:
                report.add(DoctorFinding(
                    relative_path,
                    "error",
                    f"duplicate doc_id {doc_id!r}; already used by {seen_doc_ids[doc_id]}",
                ))
            else:
                seen_doc_ids[doc_id] = relative_path
        _validate_frontmatter(report, relative_path, fm)
    return report


def run_doctor(config: Config, *, propose: bool = False) -> int:
    report = validate_vault(config, propose=propose)
    print(f"Vault: {report.vault_path}")
    print(f"Checked markdown files: {report.checked_files}")
    print(f"Indexed docs: {report.indexed_docs}")

    if not report.findings:
        print("No frontmatter issues found.")
        return 0

    print()
    for finding in report.findings:
        print(f"[{finding.severity}] {finding.relative_path}: {finding.message}")
        if finding.proposal:
            print(finding.proposal.rstrip())
            print()
    return 1 if not report.ok else 0


def _iter_markdown_files(config: Config) -> list[Path]:
    paths: list[Path] = []
    for subdir in config.indexed_dirs:
        root = config.vault_path / subdir
        if not root.is_dir():
            continue
        for path in root.rglob("*.md"):
            if "00 Templates" in path.parts or "Templates" in path.parts:
                continue
            if path.name.startswith("_"):
                continue
            paths.append(path)
    return sorted(paths)


def _validate_frontmatter(report: DoctorReport, relative_path: str, fm: dict) -> None:
    for field_name in REQUIRED_FIELDS:
        if fm.get(field_name) in (None, "", []):
            report.add(DoctorFinding(relative_path, "error", f"missing required field: {field_name}"))

    doc_id = str(fm.get("doc_id") or "")
    if doc_id and not doc_id.startswith(DOC_ID_PREFIXES):
        report.add(DoctorFinding(
            relative_path,
            "error",
            f"doc_id must start with one of: {', '.join(DOC_ID_PREFIXES)}",
        ))

    _validate_enum(report, relative_path, fm, "doc_type", DOC_TYPES)
    _validate_enum(report, relative_path, fm, "environment", ENVIRONMENTS)
    _validate_enum(report, relative_path, fm, "status", STATUSES)
    _validate_enum(report, relative_path, fm, "sensitivity", SENSITIVITIES)

    for list_field in ("tags", "related_projects", "related_assets"):
        value = fm.get(list_field)
        if value is not None and not isinstance(_coerce_list(value), list):
            report.add(DoctorFinding(relative_path, "error", f"{list_field} must be a string or list"))


def _validate_enum(
    report: DoctorReport,
    relative_path: str,
    fm: dict,
    field_name: str,
    allowed: set[str],
) -> None:
    value = fm.get(field_name)
    if value in (None, ""):
        return
    if str(value) not in allowed:
        report.add(DoctorFinding(
            relative_path,
            "error",
            f"{field_name}={value!r} must be one of: {', '.join(sorted(allowed))}",
        ))


def _proposal_for(path: Path, vault_path: Path) -> str:
    title = path.stem.replace("_", " ").replace("-", " ").strip() or "Untitled"
    prefix = _prefix_for(path)
    slug = _slugify(title)
    return f"""Suggested frontmatter:
---
doc_id: {prefix}{slug}
title: {title}
doc_type: {_doc_type_for(path)}
system: {title}
environment: other
status: draft
sensitivity: internal
tags: []
related_projects: []
related_assets: []
---
Path: {_relative(path, vault_path)}
"""


def _prefix_for(path: Path) -> str:
    parts = set(path.parts)
    if "03 Runbooks" in parts:
        return "rb-"
    if "02 Infrastructure" in parts:
        return "infra-"
    if "01 Projects" in parts:
        return "project-"
    return "note-"


def _doc_type_for(path: Path) -> str:
    parts = set(path.parts)
    if "03 Runbooks" in parts:
        return "runbook"
    if "02 Infrastructure" in parts:
        return "architecture_note"
    if "01 Projects" in parts:
        return "architecture_note"
    return "decision_record"


def _slugify(value: str) -> str:
    slug = re.sub(r"[^a-z0-9]+", "-", value.lower()).strip("-")
    return slug or "untitled"


def _relative(path: Path, root: Path) -> str:
    try:
        return str(path.relative_to(root))
    except ValueError:
        return str(path)
