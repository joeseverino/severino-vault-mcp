"""The infra-dataset registry: one catalog the MCP reads every dataset through.

`<vault>/02 Infrastructure/_infra-datasets.json` declares every structured
infra dataset once — `topology.json` (authored), the drift-guard mirrors
(reflected: DNS rewrites, proxy hosts, ACL), and doc-only references. This
module loads the registry and reads any dataset from its declared source, so an
AI session gets every infra fact through one grounded interface, each from its
true owner.

Read model (the NPM-down resilience): a read returns the git-tracked **cache**
instantly — fast, offline, always answers — with freshness metadata. With
``refresh`` it does a read-through: it asks the dataset's ``fetcher`` (the drift
guard, which owns how to talk to the live system) for current state and returns
that; if the system is unreachable it falls back to the cache, flagged stale.
Reads are pure — updating the cache is the guard's explicit ``pull``, never a
side effect of a read. Sensitivity is gated through the same policy as docs.

Adding a dataset is one registry entry. Authored vs reflected is the ownership
line: authored datasets are edited via validated writes; reflected datasets are
owned by the live system and mirrored by a guard.
"""

from __future__ import annotations

import os
import re
import subprocess
from datetime import UTC, date, datetime
from typing import TYPE_CHECKING, Any

from . import jsonio, tabular
from .atomic_write import atomic_write_text
from .frontmatter import serialize_frontmatter, split_frontmatter
from .sensitivity import Sensitivity, advisory, body_is_releasable

if TYPE_CHECKING:
    from .config import Config

# A fetcher is a drift-guard command name; we only ever run it as a fixed argv
# (`<fetcher> show`), never through a shell, and only if it is a plain name.
_FETCHER_RE = re.compile(r"[a-z0-9][a-z0-9-]*\Z")
_FETCH_TIMEOUT_S = 30


class RegistryError(ValueError):
    """Raised when the registry file is missing or malformed."""


def load_registry(config: Config) -> list[dict]:
    """Parse the registry manifest into its list of dataset declarations."""
    try:
        data = jsonio.load_file(config.infra_datasets_path, source="infra-dataset registry")
    except jsonio.JsonError as exc:
        raise RegistryError(str(exc)) from exc
    datasets = data.get("datasets") if isinstance(data, dict) else None
    if not isinstance(datasets, list):
        raise RegistryError("registry must have a 'datasets' list")
    return datasets


def _catalog_entry(d: dict) -> dict:
    """Public summary of one declaration (no body) for the list face."""
    source = d.get("source") or {}
    return {
        "id": d.get("id"),
        "kind": d.get("kind"),
        "title": d.get("title"),
        "summary": d.get("summary"),
        "owner": d.get("owner"),
        "doc": d.get("doc"),
        "sensitivity": str(Sensitivity.parse(d.get("sensitivity"))),
        "readable": source.get("type") == "file",
        "refreshable": bool(d.get("fetcher")),
    }


def list_datasets(config: Config) -> dict:
    """Envelope: the catalog of every declared dataset (metadata only)."""
    try:
        datasets = load_registry(config)
    except RegistryError as exc:
        return {"ok": False, "error": str(exc)}
    return {"ok": True, "datasets": [_catalog_entry(d) for d in datasets]}


def reflected_references(config: Config) -> list[dict]:
    """The reflected datasets, shaped for Topology.md's canonical-sources table.

    The single catalog Topology.md derives its 'not duplicated here' section
    from — the pointer list lives in the registry, not hand-copied elsewhere.
    """
    try:
        datasets = load_registry(config)
    except RegistryError:
        return []
    return [
        {"concept": d.get("title", d.get("id")), "doc": d.get("doc"), "owner": d.get("owner")}
        for d in datasets
        if d.get("kind") == "reflected"
    ]


def _iso_mtime(path) -> str | None:
    try:
        ts = path.stat().st_mtime
    except OSError:
        return None
    return datetime.fromtimestamp(ts, tz=UTC).isoformat()


def _read_cache(config: Config, source: dict) -> tuple[Any, str | None, str | None]:
    """Read a dataset's cached value. Returns (data, error, fetched_at)."""
    stype = source.get("type")
    if stype == "file":
        path = config.vault_path / source.get("path", "")
        try:
            return jsonio.load_file(path), None, _iso_mtime(path)
        except jsonio.JsonError as exc:
            return None, str(exc), None
    return None, "reference-only", None


def _fetch_live(fetcher: str) -> tuple[Any, str | None]:
    """Ask a drift guard for current live state via `<fetcher> show`.

    The guard owns the API call + credentials + normalize; this only runs it as
    a fixed argv (never a shell) and parses its JSON stdout.
    """
    if not _FETCHER_RE.match(fetcher or ""):
        return None, f"invalid fetcher {fetcher!r}"
    try:
        proc = subprocess.run(
            [fetcher, "show"],
            capture_output=True,
            text=True,
            timeout=_FETCH_TIMEOUT_S,
            env=os.environ,
            check=False,
        )
    except FileNotFoundError:
        return None, f"fetcher {fetcher!r} not found on PATH"
    except subprocess.TimeoutExpired:
        return None, f"{fetcher} show timed out after {_FETCH_TIMEOUT_S}s"
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout or "").strip().splitlines()
        return None, f"{fetcher} show failed: {detail[-1] if detail else 'nonzero exit'}"
    try:
        return jsonio.loads(proc.stdout, source=f"{fetcher} show"), None
    except jsonio.JsonError as exc:
        return None, str(exc)


def read_dataset(
    config: Config,
    dataset_id: str,
    *,
    refresh: bool = False,
) -> dict:
    """Envelope: read one dataset's data from cache, or live with ``refresh``.

    Default: the git-tracked cache + freshness (fast, offline). ``refresh``:
    try the live system via the guard, fall back to cache flagged stale if it
    is unreachable. Sensitivity-gated like a doc body: ``sensitive`` is returned
    with an advisory; ``restricted`` is withheld (releasing a restricted dataset
    would need the same interactive local unlock as ``read_doc``).
    """
    try:
        datasets = load_registry(config)
    except RegistryError as exc:
        return {"ok": False, "error": str(exc)}

    match = next((d for d in datasets if d.get("id") == dataset_id), None)
    if match is None:
        known = sorted(d.get("id") for d in datasets if d.get("id"))
        return {"ok": False, "error": f"unknown dataset {dataset_id!r}; known: {known}"}

    source = match.get("source") or {}
    sens = Sensitivity.parse(match.get("sensitivity"))
    meta = {
        "id": dataset_id,
        "kind": match.get("kind"),
        "owner": match.get("owner"),
        "doc": match.get("doc"),
        "sensitivity": str(sens),
    }

    cache_data, cache_err, fetched_at = _read_cache(config, source)

    live = False
    live_err: str | None = None
    fetcher = match.get("fetcher")
    if refresh and fetcher:
        live_data, live_err = _fetch_live(str(fetcher))
        if live_data is not None:
            cache_data, live, fetched_at = live_data, True, "live"
    elif refresh and not fetcher:
        live_err = f"{dataset_id!r} has no fetcher — cache only"

    if cache_data is None:
        return {"ok": False, **meta, "error": cache_err or live_err or "no data available"}

    result: dict[str, Any] = {"ok": True, **meta, "live": live, "fetched_at": fetched_at}
    if live_err and not live:
        result["stale"] = True
        result["refresh_error"] = live_err

    if not body_is_releasable(sens):
        result["withheld"] = True
        result["advisory"] = advisory(sens)
        return result

    result["data"] = cache_data
    if note := advisory(sens):
        result["advisory"] = note
    return result


def _markers(dataset_id: str) -> tuple[str, str]:
    return (
        f"<!-- INFRA-DATA:BEGIN {dataset_id} (generated — do not edit) -->",
        f"<!-- INFRA-DATA:END {dataset_id} -->",
    )


def _replace_region(text: str, begin: str, end: str, content: str) -> str | None:
    bi, ei = text.find(begin), text.find(end)
    if bi == -1 or ei == -1 or ei < bi:
        return None
    return f"{text[:bi + len(begin)]}\n{content}\n{text[ei:]}"


def _under_vault(config: Config, rel: str):
    path = (config.vault_path / rel).resolve()
    try:
        path.relative_to(config.vault_path.resolve())
    except ValueError:
        return None
    return path


def write_dataset(config: Config, dataset_id: str, payload_text: str) -> dict:
    """Write a reflected dataset's live state: the JSON cache + the doc view.

    The canonical write behind a guard's ``pull``. Writes the ``.json`` cache
    (sorted, stable diff), regenerates the doc's generated table region from the
    declared columns, and stamps ``last_reviewed`` — all from one normalized
    payload, so the file and the human table can never drift.
    """
    try:
        datasets = load_registry(config)
    except RegistryError as exc:
        return {"ok": False, "error": str(exc)}

    match = next((d for d in datasets if d.get("id") == dataset_id), None)
    if match is None:
        return {"ok": False, "error": f"unknown dataset {dataset_id!r}"}

    source = match.get("source") or {}
    if source.get("type") != "file":
        return {"ok": False, "error": f"{dataset_id!r} is not a file-backed dataset"}

    try:
        data = jsonio.loads(payload_text, source=f"{dataset_id} payload")
    except jsonio.JsonError as exc:
        return {"ok": False, "error": str(exc)}

    json_path = _under_vault(config, source.get("path", ""))
    if json_path is None:
        return {"ok": False, "error": f"cache path escapes vault: {source.get('path')!r}"}
    try:
        atomic_write_text(json_path, jsonio.canonical(data) + "\n")
    except OSError as exc:
        return {"ok": False, "error": f"cache write failed: {exc}"}

    records = len(data) if isinstance(data, list) else None
    result: dict[str, Any] = {"ok": True, "id": dataset_id, "wrote": source["path"], "records": records}

    render = match.get("render") or {}
    doc_rel, columns = render.get("doc_path"), render.get("columns")
    if not (doc_rel and columns and isinstance(data, list)):
        return result  # json-only dataset (e.g. non-tabular ACL): no doc table

    doc_path = _under_vault(config, doc_rel)
    if doc_path is None:
        result["doc_warning"] = f"doc path escapes vault: {doc_rel!r}"
        return result
    try:
        doc_text = doc_path.read_text(encoding="utf-8")
    except OSError as exc:
        result["doc_warning"] = f"could not read doc: {exc}"
        return result

    begin, end = _markers(dataset_id)
    new_text = _replace_region(doc_text, begin, end, tabular.render_table(data, columns))
    if new_text is None:
        result["doc_warning"] = f"no INFRA-DATA region for {dataset_id} in {doc_rel}"
        return result

    frontmatter, body, _start = split_frontmatter(new_text)
    if frontmatter is not None:
        frontmatter["last_reviewed"] = date.today().isoformat()
        new_text = serialize_frontmatter(frontmatter) + body
        result["reviewed"] = frontmatter["last_reviewed"]
    try:
        atomic_write_text(doc_path, new_text)
    except OSError as exc:
        result["doc_warning"] = f"doc write failed: {exc}"
        return result
    result["doc_updated"] = doc_rel
    return result
