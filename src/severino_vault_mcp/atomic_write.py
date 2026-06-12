"""Durable file replacement primitives shared by every vault writer.

Both the single-file generic frontmatter writers and the multi-file writeup
transaction need the same staged-tempfile + ``fsync`` + ``os.replace`` dance to
avoid truncating a doc on a failed write. Centralizing it here means there is
one implementation of "replace a file durably," and the transactional path is
just the locked, multi-file generalization of :func:`atomic_write_text`.
"""

from __future__ import annotations

import fcntl
import hashlib
import os
import tempfile
from contextlib import suppress
from pathlib import Path


def _stage_sibling(path: Path, data: bytes, *, prefix: str) -> Path:
    """Write ``data`` to a flushed, fsynced sibling temp file and return it.

    Staging in the target's own directory keeps the later ``os.replace`` atomic
    (same filesystem) and leaves the original intact until the rename succeeds.
    """
    with tempfile.NamedTemporaryFile(
        mode="wb",
        dir=path.parent,
        prefix=prefix,
        delete=False,
    ) as handle:
        handle.write(data)
        handle.flush()
        os.fsync(handle.fileno())
        return Path(handle.name)


def atomic_write_text(path: Path, text: str) -> None:
    """Durably replace one text file through a sibling temporary file."""
    staged: Path | None = None
    try:
        staged = _stage_sibling(
            path,
            text.encode("utf-8"),
            prefix=f".{path.name}.svmc-",
        )
        os.replace(staged, path)
        staged = None
    finally:
        if staged is not None:
            with suppress(FileNotFoundError):
                staged.unlink()


def transactional_replace(
    root: Path,
    replacements: dict[Path, str],
) -> tuple[bool, str | None]:
    """Stage all replacements, then replace under a lock with rollback.

    Every target is staged first; under an exclusive lock keyed to ``root`` the
    files are checked for concurrent modification and then replaced. Any failure
    rolls back the files already swapped, so the set lands all-or-nothing.
    """
    if not replacements:
        return True, None

    originals = {path: path.read_bytes() for path in replacements}
    staged: dict[Path, Path] = {}
    replaced: list[Path] = []
    lock_key = hashlib.sha256(str(root.resolve()).encode()).hexdigest()[:16]
    lock_path = Path(tempfile.gettempdir()) / f"svmc-writeups-{lock_key}.lock"
    try:
        for path, text in replacements.items():
            staged[path] = _stage_sibling(
                path,
                text.encode("utf-8"),
                prefix=f".{path.name}.svmc-",
            )

        with lock_path.open("a+b") as lock_handle:
            fcntl.flock(lock_handle.fileno(), fcntl.LOCK_EX)
            try:
                for path, original in originals.items():
                    if path.read_bytes() != original:
                        raise RuntimeError(
                            f"file changed during transaction: {path}"
                        )
                for path in sorted(replacements, key=str):
                    os.replace(staged[path], path)
                    replaced.append(path)
            except (OSError, RuntimeError) as exc:
                rollback_errors: list[str] = []
                for path in reversed(replaced):
                    try:
                        rollback_path = _stage_sibling(
                            path,
                            originals[path],
                            prefix=f".{path.name}.rollback-",
                        )
                        os.replace(rollback_path, path)
                    except OSError as rollback_exc:
                        rollback_errors.append(f"{path}: {rollback_exc}")
                detail = str(exc)
                if rollback_errors:
                    detail += "; rollback errors: " + "; ".join(rollback_errors)
                return False, detail
            finally:
                fcntl.flock(lock_handle.fileno(), fcntl.LOCK_UN)
        return True, None
    except OSError as exc:
        return False, str(exc)
    finally:
        for path in staged.values():
            with suppress(FileNotFoundError):
                path.unlink()


__all__ = ["atomic_write_text", "transactional_replace"]
