#!/usr/bin/env python3
"""Compare the usage skill's CLI flag table with the shipped binary.

The binary's leaf ``--help`` output is the source of truth. This checker only
reads the fenced CLI table from the skill and compares flag names; it does not
parse or modify Go CLI code.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path


FLAG = re.compile(r"(?<![\w-])(--?[a-z][a-z0-9-]*)\b")
TABLE_HEADING = "## The CLI ("


def flag_name(flag: str) -> str:
    """Compare flag names without caring whether help prints one dash."""
    return flag.lstrip("-")


NESTED = {"matrix", "check", "finding", "parse-agent-id", "record", "selftest-verdict", "fold"}


def table_lines(skill: Path) -> list[str]:
    lines = skill.read_text(encoding="utf-8").splitlines()
    try:
        start = next(i for i, line in enumerate(lines) if line.startswith(TABLE_HEADING))
    except StopIteration as exc:
        raise ValueError(f"{skill}: CLI heading not found") from exc
    try:
        fence = next(i for i in range(start + 1, len(lines)) if lines[i].strip() == "```text")
    except StopIteration as exc:
        raise ValueError(f"{skill}: CLI table fence not found") from exc
    end = next((i for i in range(fence + 1, len(lines)) if lines[i].strip() == "```"), None)
    if end is None:
        raise ValueError(f"{skill}: CLI table fence is not closed")
    return lines[fence + 1 : end]


def command_and_flags(line: str) -> tuple[tuple[str, ...], set[str]] | None:
    body = line.split("#", 1)[0].strip()
    if not body:
        return None
    words = body.split()
    if not words or words[0].startswith("|"):
        return None
    command = [words[0]]
    if len(words) > 1 and words[1] in NESTED:
        command.append(words[1])
    return tuple(command), {flag_name(flag) for flag in FLAG.findall(body)}


def help_flags(binary: Path, command: tuple[str, ...]) -> set[str]:
    result = subprocess.run(
        [str(binary), *command, "--help"],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )
    if result.returncode:
        raise RuntimeError(
            f"{' '.join(command)} --help exited {result.returncode}: {result.stdout.strip()}"
        )
    return {
        flag_name(flag)
        for line in result.stdout.splitlines()
        for flag in FLAG.findall(line)
        if line.lstrip().startswith("-")
    }


def check(skill: Path, binary: Path) -> int:
    expected: dict[tuple[str, ...], set[str]] = {}
    for line in table_lines(skill):
        parsed = command_and_flags(line)
        if parsed is None:
            continue
        command, flags = parsed
        if command in expected:
            raise ValueError(f"duplicate CLI table command: {' '.join(command)}")
        expected[command] = flags

    failures = 0
    for command, documented in expected.items():
        actual = help_flags(binary, command)
        missing = sorted(actual - documented)
        extra = sorted(documented - actual)
        if missing or extra:
            failures += 1
            print(f"FAIL {' '.join(command)}")
            if missing:
                print("  missing from skill: " + " ".join(missing))
            if extra:
                print("  not accepted by binary: " + " ".join(extra))
        else:
            print(f"PASS {' '.join(command)} ({len(actual)} flags)")

    print(f"cli-table: {'FAIL' if failures else 'PASS'} — {len(expected)} command(s), {failures} mismatch(es)")
    return 1 if failures else 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--skill", type=Path, default=Path("skills/mischief-usage/SKILL.md"))
    parser.add_argument("--binary", type=Path, default=Path("bin/mischief"))
    args = parser.parse_args(argv)
    try:
        if not args.skill.is_file():
            raise ValueError(f"skill file not found: {args.skill}")
        if not args.binary.is_file():
            raise ValueError(f"binary not found: {args.binary}; run `make bin` first")
        return check(args.skill, args.binary)
    except (OSError, RuntimeError, ValueError) as exc:
        print(f"cli-table: ERROR — {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
