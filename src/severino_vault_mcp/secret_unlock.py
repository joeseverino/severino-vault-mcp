"""Local unlock gate for restricted document bodies.

The LLM can request a restricted body by setting `include_restricted=True`,
but the local machine still decides whether
to release it. The unlock phrase is collected through a macOS hidden-input
dialog, never through chat.
"""

from __future__ import annotations

import hashlib
import hmac
import os
import shutil
import subprocess
import sys
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path


@dataclass(frozen=True)
class SecretUnlockResult:
    allowed: bool
    result: str
    message: str

    def to_dict(self) -> dict[str, str | bool]:
        return {
            "allowed": self.allowed,
            "result": self.result,
            "message": self.message,
        }


def verify_unlock_phrase(phrase: str, encoded_hash: str) -> bool:
    """Verify a phrase against `sha256:<salt_hex>:<digest_hex>`."""
    try:
        algorithm, salt_hex, expected_hex = encoded_hash.strip().split(":", 2)
        if algorithm != "sha256":
            return False
        salt = bytes.fromhex(salt_hex)
        expected = bytes.fromhex(expected_hex)
    except ValueError:
        return False

    actual = hashlib.sha256(salt + phrase.encode("utf-8")).digest()
    return hmac.compare_digest(actual, expected)


def load_unlock_hash(
    *,
    env_hash: str | None,
    hash_file: Path,
    keychain_service: str,
    keychain_account: str,
) -> str | None:
    """Load the salted unlock hash from env, a local file, or macOS Keychain."""
    if env_hash:
        return env_hash.strip()

    try:
        if hash_file.is_file():
            return hash_file.read_text(encoding="utf-8").strip()
    except OSError:
        return None

    if sys.platform != "darwin" or not shutil.which("security"):
        return None

    try:
        proc = subprocess.run(
            [
                "security",
                "find-generic-password",
                "-s",
                keychain_service,
                "-a",
                keychain_account,
                "-w",
            ],
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None

    if proc.returncode != 0:
        return None
    return proc.stdout.strip()


def prompt_unlock_phrase(doc_id: str, title: str) -> str | None:
    """Prompt locally for the unlock phrase using a macOS hidden-input dialog."""
    if sys.platform != "darwin" or not shutil.which("osascript"):
        return None

    label = _applescript_string(f"Unlock restricted doc?\n\n{title}\n{doc_id}")
    script = (
        f'display dialog {label} default answer "" with hidden answer '
        'buttons {"Cancel", "Unlock"} default button "Unlock"'
    )
    try:
        proc = subprocess.run(
            ["osascript", "-e", script, "-e", "text returned of result"],
            capture_output=True,
            text=True,
            timeout=60,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None

    if proc.returncode != 0:
        return None
    return proc.stdout.rstrip("\n")


def _append_audit_line(audit_log_path: Path, line: str) -> None:
    """Append one 0600 audit line. Never write body content or phrases.

    Swallows OSError: audit logging must never turn a successful local action
    into a failure. Callers still report their own result to the caller.
    """
    try:
        audit_log_path.parent.mkdir(parents=True, exist_ok=True)
        with audit_log_path.open("a", encoding="utf-8") as handle:
            handle.write(line if line.endswith("\n") else line + "\n")
        os.chmod(audit_log_path, 0o600)
    except OSError:
        pass


def audit_event(audit_log_path: Path, *, action: str, detail: str = "") -> None:
    """Append one local audit line for a non-unlock local action.

    Used for events like contact-PII reveal: records that PII was released and
    how many rows, never the PII itself.
    """
    timestamp = datetime.now(UTC).isoformat()
    suffix = f" {detail}" if detail else ""
    _append_audit_line(
        audit_log_path,
        f"{timestamp} action={action}{suffix} client=stdio",
    )


def audit_secret_unlock(audit_log_path: Path, *, doc_id: str, result: str) -> None:
    """Append one local audit line. Never write body content or phrases."""
    timestamp = datetime.now(UTC).isoformat()
    _append_audit_line(
        audit_log_path,
        f"{timestamp} action=restricted_unlock doc_id={doc_id} "
        f"result={result} client=stdio",
    )


def _applescript_string(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'
