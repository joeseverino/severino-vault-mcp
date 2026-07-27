"""jseverino.com content contract projection.

The canonical contract lives in ``jseverino.com/contracts/content.v1.json``.
That repository emits the bundled projection consumed here; the fingerprint
makes lineage explicit while keeping this package self-contained and offline.
"""

from __future__ import annotations

import inspect
import json
from functools import lru_cache
from importlib.resources import files
from typing import Any


@lru_cache(maxsize=1)
def projection() -> dict[str, Any]:
    resource = files(__package__).joinpath("site_content.v1.json")
    return json.loads(resource.read_text(encoding="utf-8"))


def writeup_fields() -> dict[str, dict[str, Any]]:
    return projection()["contract"]["collections"]["writeups"]["fields"]


def editable_writeup_fields() -> dict[str, dict[str, Any]]:
    return {
        name: spec
        for name, spec in writeup_fields().items()
        if spec.get("editable") is True
    }


def mutable_scalar_fields() -> frozenset[str]:
    return frozenset(editable_writeup_fields())


def _python_type(spec: dict[str, Any]) -> type:
    return {
        "boolean": bool,
        "integer": int,
        "string[]": list,
    }.get(str(spec.get("type")), str)


def update_tool_signature() -> inspect.Signature:
    """Build FastMCP's public tool schema from the site-owned field contract."""
    parameters = [
        inspect.Parameter(
            "slug",
            inspect.Parameter.POSITIONAL_OR_KEYWORD,
            annotation=str,
        )
    ]
    for name, spec in editable_writeup_fields().items():
        annotation = bool | None if name == "published" else _python_type(spec) | None
        parameters.append(
            inspect.Parameter(
                name,
                inspect.Parameter.KEYWORD_ONLY,
                default=None,
                annotation=annotation,
            )
        )
    parameters.append(
        inspect.Parameter(
            "touch_last_reviewed",
            inspect.Parameter.KEYWORD_ONLY,
            default=False,
            annotation=bool,
        )
    )
    return inspect.Signature(parameters, return_annotation=dict[str, Any])


def cli_fields() -> list[tuple[str, dict[str, Any]]]:
    return [
        (name, spec)
        for name, spec in editable_writeup_fields().items()
        if spec.get("cli_flag")
    ]


def public_contract() -> dict[str, Any]:
    data = projection()
    return {
        "ok": True,
        "source": data["source"],
        "fingerprint": data["fingerprint"],
        "contract": data["contract"],
    }
