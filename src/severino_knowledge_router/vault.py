"""
Vault loader.

Walks `<vault>/01 Projects/`, `<vault>/02 Infrastructure/`, `<vault>/03 Runbooks/`
(configurable), parses YAML frontmatter from every `.md`, and caches an in-memory
index for the configured `cache_seconds`. The frontmatter parser is hand-rolled
to avoid a PyYAML dependency — it understands the constrained shape documented
at `02 Infrastructure/Severino HQ/Frontmatter Schema.md` in the vault.
"""

from __future__ import annotations

import re
import time
from dataclasses import dataclass, field
from pathlib import Path

from .config import Config
from .sensitivity import Sensitivity


@dataclass
class Doc:
    doc_id: str
    title: str
    doc_type: str
    system: str
    environment: str
    status: str
    sensitivity: Sensitivity
    last_reviewed: str | None
    tags: list[str]
    related_projects: list[str]
    related_assets: list[str]
    path: Path                 # absolute path on disk
    relative_path: str         # vault-root-relative
    body: str                  # markdown body (after frontmatter)

    def to_metadata(self) -> dict:
        """Lossless metadata view — never includes the body."""
        return {
            "doc_id": self.doc_id,
            "title": self.title,
            "doc_type": self.doc_type,
            "system": self.system,
            "environment": self.environment,
            "status": self.status,
            "sensitivity": self.sensitivity.value,
            "last_reviewed": self.last_reviewed,
            "tags": list(self.tags),
            "related_projects": list(self.related_projects),
            "related_assets": list(self.related_assets),
            "obsidian_path": self.relative_path,
        }


# ----- Frontmatter parsing -----------------------------------------------------

def _scalar(token: str):
    token = token.strip()
    if len(token) >= 2 and token[0] == token[-1] and token[0] in ('"', "'"):
        token = token[1:-1]
    if token in ("null", "~", ""):
        return None
    if token == "true":
        return True
    if token == "false":
        return False
    return token


def _split_inline_list(inner: str) -> list[str]:
    if not inner:
        return []
    out, depth, buf = [], 0, []
    for ch in inner:
        if ch == "," and depth == 0:
            out.append("".join(buf).strip())
            buf = []
        else:
            buf.append(ch)
            if ch in "[{":
                depth += 1
            elif ch in "]}":
                depth -= 1
    out.append("".join(buf).strip())
    return [x for x in out if x]


def _parse_yaml_block(block: str) -> dict:
    data: dict = {}
    current_list_key: str | None = None
    for raw in block.splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            current_list_key = None
            continue
        if current_list_key and re.match(r"^\s+-\s+", raw):
            data.setdefault(current_list_key, []).append(_scalar(raw.strip()[2:].strip()))
            continue
        m = re.match(r"^([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.*)$", raw)
        if not m:
            current_list_key = None
            continue
        key, value = m.group(1), m.group(2).strip()
        if value == "":
            current_list_key = key
            data[key] = []
            continue
        current_list_key = None
        if value.startswith("[") and value.endswith("]"):
            data[key] = [_scalar(x) for x in _split_inline_list(value[1:-1].strip())]
            continue
        data[key] = _scalar(value)
    return data


def _split_frontmatter(text: str) -> tuple[dict | None, str]:
    """Return (frontmatter_dict, body) or (None, full_text) if not present."""
    if not text.lstrip().startswith("---"):
        return None, text
    lines = text.splitlines()
    # Find the first '---' (start)
    start = next((i for i, line in enumerate(lines) if line.strip() == "---"), None)
    if start is None:
        return None, text
    # Find the next '---' (end)
    end = next(
        (i for i in range(start + 1, len(lines)) if lines[i].strip() == "---"),
        None,
    )
    if end is None:
        return None, text
    fm = _parse_yaml_block("\n".join(lines[start + 1:end]))
    body = "\n".join(lines[end + 1:]).lstrip("\n")
    return fm, body


def _coerce_list(value) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [str(v) for v in value if v is not None]
    return [str(value)]


# ----- Index -----------------------------------------------------------------

@dataclass
class Index:
    docs: list[Doc] = field(default_factory=list)
    by_doc_id: dict[str, Doc] = field(default_factory=dict)
    loaded_at: float = 0.0

    def add(self, doc: Doc) -> None:
        self.docs.append(doc)
        self.by_doc_id[doc.doc_id] = doc


class VaultLoader:
    """Loads and caches the vault index. Thread-unsafe (single-process MCP)."""

    def __init__(self, config: Config) -> None:
        self.config = config
        self._index: Index | None = None

    def index(self, force: bool = False) -> Index:
        now = time.time()
        if (
            not force
            and self._index is not None
            and (now - self._index.loaded_at) < self.config.cache_seconds
        ):
            return self._index
        self._index = self._build()
        self._index.loaded_at = now
        return self._index

    def _build(self) -> Index:
        idx = Index()
        for sub in self.config.indexed_dirs:
            root = self.config.vault_path / sub
            if not root.is_dir():
                continue
            for path in sorted(root.rglob("*.md")):
                if "00 Templates" in path.parts or "Templates" in path.parts:
                    continue
                if path.name.startswith("_"):
                    continue
                try:
                    text = path.read_text(encoding="utf-8", errors="replace")
                except OSError:
                    continue
                fm, body = _split_frontmatter(text)
                if not fm or not fm.get("doc_id"):
                    continue
                idx.add(self._mk_doc(path, fm, body))
        return idx

    def _mk_doc(self, path: Path, fm: dict, body: str) -> Doc:
        return Doc(
            doc_id=str(fm["doc_id"]),
            title=str(fm.get("title") or fm["doc_id"]),
            doc_type=str(fm.get("doc_type") or "runbook"),
            system=str(fm.get("system") or ""),
            environment=str(fm.get("environment") or "other"),
            status=str(fm.get("status") or "active"),
            sensitivity=Sensitivity.parse(fm.get("sensitivity")),
            last_reviewed=(str(fm["last_reviewed"]) if fm.get("last_reviewed") else None),
            tags=_coerce_list(fm.get("tags")),
            related_projects=_coerce_list(fm.get("related_projects")),
            related_assets=_coerce_list(fm.get("related_assets")),
            path=path,
            relative_path=str(path.relative_to(self.config.vault_path)),
            body=body,
        )
