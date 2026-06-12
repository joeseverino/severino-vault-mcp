"""
Vault loader.

Walks `<vault>/01 Projects/`, `<vault>/02 Infrastructure/`, `<vault>/03 Runbooks/`
(configurable), parses YAML frontmatter from every `.md`, and caches an in-memory
index for the configured `cache_seconds`. The shared parser in
`frontmatter.py` avoids a PyYAML dependency and understands the constrained
shape documented at
`02 Infrastructure/Operations Metadata/Frontmatter Schema.md` in the vault.

If `fd` (https://github.com/sharkdp/fd) is on PATH, it's used to walk the vault
because it's much faster than Python's pathlib at scale. Otherwise we fall
back to `Path.rglob`.
"""

from __future__ import annotations

import shutil
import subprocess
import time
import tomllib
from dataclasses import dataclass, field
from pathlib import Path

from .config import Config
from .frontmatter import split_frontmatter
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
    body_start_line: int = 1   # 1-indexed line in the source file where the body begins
                               # (line after the closing `---`); ripgrep-based search
                               # uses this to skip matches that fall inside frontmatter.

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


def _coerce_list(value) -> list[str]:
    if value is None:
        return []
    if isinstance(value, list):
        return [str(v) for v in value if v is not None]
    return [str(value)]


def _normalize_alias(value: str) -> str:
    return " ".join(value.strip().lower().split())


# ----- Index -----------------------------------------------------------------

@dataclass
class Index:
    docs: list[Doc] = field(default_factory=list)
    by_doc_id: dict[str, Doc] = field(default_factory=dict)
    aliases: dict[str, str] = field(default_factory=dict)
    invalid_aliases: dict[str, str] = field(default_factory=dict)
    duplicate_doc_ids: dict[str, list[str]] = field(default_factory=dict)
    loaded_at: float = 0.0

    def add(self, doc: Doc) -> None:
        existing = self.by_doc_id.get(doc.doc_id)
        if existing is not None:
            paths = self.duplicate_doc_ids.setdefault(
                doc.doc_id,
                [existing.relative_path],
            )
            paths.append(doc.relative_path)
            self.by_doc_id.pop(doc.doc_id, None)
            self.docs = [
                candidate
                for candidate in self.docs
                if candidate.doc_id != doc.doc_id
            ]
            return
        if doc.doc_id in self.duplicate_doc_ids:
            self.duplicate_doc_ids[doc.doc_id].append(doc.relative_path)
            return
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
            for path in self._walk(root):
                if "00 Templates" in path.parts or "Templates" in path.parts:
                    continue
                if path.name.startswith("_"):
                    continue
                try:
                    text = path.read_text(encoding="utf-8", errors="replace")
                except OSError:
                    continue
                fm, body, body_start_line = split_frontmatter(text)
                if not fm:
                    continue
                if not fm.get("doc_id"):
                    # Reference docs use a slim frontmatter shape
                    # (`type: reference, tags, created`) without doc_id.
                    # Synthesize one from the file path so they're searchable
                    # alongside HQ-shape runbooks and decision records.
                    if str(fm.get("type") or "").strip().lower() == "reference":
                        slug = path.stem.lower().replace(" ", "-").replace("_", "-")
                        fm["doc_id"] = f"ref-{slug}"
                        fm.setdefault("doc_type", "reference")
                        fm.setdefault("sensitivity", "public")
                    else:
                        continue
                idx.add(self._mk_doc(path, fm, body, body_start_line))
        idx.aliases, idx.invalid_aliases = self._load_aliases(idx)
        return idx

    def _load_aliases(self, idx: Index) -> tuple[dict[str, str], dict[str, str]]:
        """Load optional local aliases as normalized phrase -> doc_id."""
        try:
            with self.config.aliases_path.open("rb") as handle:
                data = tomllib.load(handle)
        except FileNotFoundError:
            return {}, {}
        except (OSError, tomllib.TOMLDecodeError):
            return {}, {}

        raw_aliases = data.get("aliases", {})
        if not isinstance(raw_aliases, dict):
            return {}, {}

        aliases: dict[str, str] = {}
        invalid: dict[str, str] = {}
        for raw_alias, raw_doc_id in raw_aliases.items():
            alias = _normalize_alias(str(raw_alias))
            doc_id = str(raw_doc_id).strip()
            if not alias or not doc_id:
                continue
            if doc_id in idx.by_doc_id:
                aliases[alias] = doc_id
            else:
                invalid[alias] = doc_id
        return aliases, invalid

    def _walk(self, root: Path) -> list[Path]:
        """List `.md` files under `root`. Uses `fd` if available, else pathlib."""
        fd = shutil.which("fd")
        if fd:
            try:
                proc = subprocess.run(
                    [fd, "--type", "f", "--extension", "md", ".", str(root)],
                    capture_output=True,
                    text=True,
                    timeout=10,
                    check=True,
                )
                return sorted(Path(line) for line in proc.stdout.splitlines() if line)
            except (subprocess.SubprocessError, OSError):
                # Fall through to the pathlib fallback below.
                pass
        return sorted(root.rglob("*.md"))

    def _mk_doc(self, path: Path, fm: dict, body: str, body_start_line: int) -> Doc:
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
            body_start_line=body_start_line,
        )
