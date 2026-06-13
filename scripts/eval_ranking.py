#!/usr/bin/env python3
"""Rank-quality eval for `find_runbook`, scored against real ground truth.

The vault's Quick Index ("How do I..." / "When something's broken" tables) is a
hand-maintained map of natural-language intent -> the canonical doc for it. That
is exactly the retrieval task `find_runbook` performs, so it doubles as a
labeled eval set: every `| intent | ... | [[Doc]] |` row is one (query, expected
doc_id) case.

Run it against the live vault:

    SVMC_VAULT_PATH="$NOTES_HOME" python scripts/eval_ranking.py
    python scripts/eval_ranking.py --misses     # also print every miss

It prints top-1 / top-3 / top-5 accuracy. This is a *script*, not a CI test —
it needs the real vault, which isn't in the repo. The committed unit tests in
tests/test_search.py lock the scoring behaviors this eval drove
(stopword filtering, the capped body signal) against the fixture vault.

Misses cluster into two honest buckets, and the script labels neither for you:
a ranker bug (fix the scorer) vs. a doc-metadata gap (the target doc is missing
a tag/system term the intent uses — fix the doc, not the code).
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "src"))

from severino_vault_mcp.config import Config  # noqa: E402
from severino_vault_mcp.search import rank  # noqa: E402
from severino_vault_mcp.vault import VaultLoader  # noqa: E402

QUICK_INDEX_DOC_ID = "report-playbook-mcp-index"
_ROW = re.compile(r"^\|([^|]+)\|([^|]+)\|([^|]*\[\[[^\]]+\]\][^|]*)\|", re.M)
_LINK = re.compile(r"\[\[([^\]#|]+)")


def build_cases(idx) -> list[tuple[str, str]]:
    """Extract (intent, expected_doc_id) pairs from the Quick Index tables."""
    by_title = {d.title.lower(): d for d in idx.docs}
    by_stem = {
        os.path.splitext(os.path.basename(d.relative_path))[0].lower(): d
        for d in idx.docs
    }
    quick_index = idx.by_doc_id.get(QUICK_INDEX_DOC_ID)
    if quick_index is None:
        return []

    cases: list[tuple[str, str]] = []
    for match in _ROW.finditer(quick_index.body):
        intent = match.group(1).strip()
        if intent.lower() in ("intent", "---", "") or intent.startswith("-"):
            continue
        link = _LINK.search(match.group(3))
        if not link:
            continue
        target = link.group(1).strip().lower()
        doc = by_title.get(target) or by_stem.get(target)
        if doc is not None:
            cases.append((intent, doc.doc_id))
    # De-dupe identical (query, expected) rows that appear in multiple tables.
    return list(dict.fromkeys(cases))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--misses", action="store_true", help="Print each miss with its top-3."
    )
    args = parser.parse_args()

    loader = VaultLoader(Config.from_env())
    idx = loader.index()
    cases = build_cases(idx)
    if not cases:
        print("No eval cases found — is the Quick Index present in the vault?")
        return 1

    top1 = top3 = top5 = miss = 0
    misses: list[tuple[str, str, list[str]]] = []
    for query, expected in cases:
        ids = [hit.doc.doc_id for hit in rank(idx.docs, query, limit=5)]
        if ids[:1] == [expected]:
            top1 += 1
        elif expected in ids[:3]:
            top3 += 1
        elif expected in ids:
            top5 += 1
        else:
            miss += 1
            misses.append((query, expected, ids[:3]))

    n = len(cases)
    print(f"{n} eval cases (from the Quick Index, {len(idx.docs)} docs indexed)\n")
    print(f"  top-1 : {top1}/{n}  ({top1 / n:.0%})")
    print(f"  top-3 : {top1 + top3}/{n}  ({(top1 + top3) / n:.0%})")
    print(f"  top-5 : {top1 + top3 + top5}/{n}  ({(top1 + top3 + top5) / n:.0%})")
    print(f"  miss  : {miss}/{n}")

    if args.misses and misses:
        print("\n--- misses (query -> want | got top-3) ---")
        for query, expected, got in misses:
            print(f"  {query!r}\n    want {expected}\n    got  {got}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
