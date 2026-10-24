#!/usr/bin/env python3
"""Reject staged board snapshots that silently lose existing rows.

The index is the commit candidate; the working tree may contain a different
version of the board.  Compare the staged blob with the current commit (the
parent tree of the commit being prepared), not with the working-tree file.

A deliberate destructive rewrite can be recorded with --ack-dropped.  The
pre-commit hook never passes that flag, so an acknowledgement is an explicit
one-off operator choice rather than an accidental green commit.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path


def git(repo: Path, *args: str) -> bytes:
    result = subprocess.run(
        ["git", "-C", str(repo), *args],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if result.returncode:
        detail = result.stderr.decode("utf-8", "replace").strip()
        raise RuntimeError(f"git {' '.join(args)} failed: {detail}")
    return result.stdout


def repository_root() -> Path:
    return Path(git(Path.cwd(), "rev-parse", "--show-toplevel").decode().strip())


def relative_board_path(repo: Path, requested: str) -> str:
    path = Path(requested)
    if not path.is_absolute():
        path = Path.cwd() / path
    try:
        return path.resolve().relative_to(repo.resolve()).as_posix()
    except ValueError as exc:
        raise RuntimeError(f"board path is outside repository: {requested}") from exc


def parse_rows(data: bytes, source: str) -> tuple[set[str], int]:
    ids: set[str] = set()
    count = 0
    for line_number, raw_line in enumerate(data.splitlines(), 1):
        if not raw_line.strip():
            continue
        try:
            row = json.loads(raw_line)
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"{source}:{line_number}: invalid JSON: {exc.msg}") from exc
        if not isinstance(row, dict) or not isinstance(row.get("id"), str) or not row["id"]:
            raise RuntimeError(f"{source}:{line_number}: row must contain a non-empty string id")
        ids.add(row["id"])
        count += 1
    return ids, count


def staged_blob(repo: Path, path: str) -> bytes:
    # `git show :path` reads the index, which is the exact commit candidate.
    try:
        return git(repo, "show", f":{path}")
    except RuntimeError as exc:
        raise RuntimeError(f"staged board blob is unavailable for {path}: {exc}") from exc


def parent_blob(repo: Path, path: str) -> bytes | None:
    # During pre-commit, HEAD is the parent tree of the proposed commit.
    result = subprocess.run(
        ["git", "-C", str(repo), "show", f"HEAD:{path}"],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if result.returncode:
        return None
    return result.stdout


def check(repo: Path, path: str, acknowledge: bool) -> int:
    staged_ids, staged_count = parse_rows(staged_blob(repo, path), f"index:{path}")
    previous = parent_blob(repo, path)
    if previous is None:
        print(f"idset: PASS — no parent board for {path}")
        return 0

    previous_ids, previous_count = parse_rows(previous, f"HEAD:{path}")
    dropped = sorted(previous_ids - staged_ids)
    count_decreased = staged_count < previous_count

    if not dropped and not count_decreased:
        print(
            f"idset: PASS — {path}: {previous_count} parent row(s), "
            f"{staged_count} staged row(s)"
        )
        return 0

    if acknowledge:
        print(
            f"idset: ACKNOWLEDGED — {path}: {previous_count} parent row(s), "
            f"{staged_count} staged row(s)"
        )
        if dropped:
            print("idset: dropped ids acknowledged: " + ", ".join(dropped))
        return 0

    print(
        f"idset: FAIL — {path}: staged board loses rows from parent "
        f"({previous_count} -> {staged_count})",
        file=sys.stderr,
    )
    if dropped:
        print("idset: dropped ids: " + ", ".join(dropped), file=sys.stderr)
    elif count_decreased:
        print("idset: dropped ids: none (row count decreased)", file=sys.stderr)
    print("idset: use --ack-dropped only for an explicitly acknowledged rewrite", file=sys.stderr)
    return 1


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("board", help="board JSONL path")
    parser.add_argument(
        "--ack-dropped",
        action="store_true",
        help="explicitly acknowledge dropped rows for this invocation",
    )
    args = parser.parse_args(argv)
    try:
        repo = repository_root()
        path = relative_board_path(repo, args.board)
        return check(repo, path, args.ack_dropped)
    except RuntimeError as exc:
        print(f"idset: ERROR — {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
