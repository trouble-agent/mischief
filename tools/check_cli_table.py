#!/usr/bin/env python3
"""Compare the usage skill's CLI table with the shipped binary.

The binary's own help/refusal output is the source of truth. This checker
reads the fenced CLI table from the skill and runs three gates; it does not
parse or modify Go CLI code.

1. Per-command flags: every table command is driven with ``--help`` and its
   documented flags are compared with the help-derived flag set — hard FAIL
   in both directions (a flag the help lists but the skill omits, or a flag
   the skill documents that the binary does not accept). The accepted set is
   the help-derived flags PLUS the probe-discovered flags of gate 3, so a
   documented flag is not falsely rejected for verbs whose ``--help`` lists
   no flags at all; for every FlagSet-driven verb the probe is empty and the
   comparison is byte-for-byte the original one.

2. Verb coverage (DOC-35): the shipped verb set is derived from the binary's
   own top-level usage lines (``  mischief <verb> ...``) and every shipped
   verb must appear as the first token of some table command, hard FAIL
   naming each missing verb. ``PENDING_TABLE_VERBS`` below is the documented
   exception: verbs that ship but whose table row is still being written in
   another branch are labeled WARNings so the committed tree stays green.

3. Flag-gap probe (DOC-35): a verb may parse its own arguments without
   listing flags in ``--help`` — ``version`` does (MISCHIEF-DF-72), so
   ``version --json`` is a real, accepted flag while ``version --help``
   prints no flag list at all, and gate 1 saw 0 flags and printed
   "PASS version (0 flags)" for a genuinely undocumented flag. For every
   table command the checker probes with an intentionally unknown flag and
   parses the binary's named refusal ("supported: --json, --version", the
   MISCHIEF-CLI-001 refusal shape) to discover flags accepted beyond the
   help list. Accepted-but-undocumented flags are reported as a WARN line
   (exit stays 0) because the current table genuinely does not list
   ``version --json`` yet — the committed state must stay green (the brief
   forbids editing skills/mischief-usage/SKILL.md here); the red-proof
   fixture table with the flag added clears the warning and shows the check
   engaging. A flag documented but NOT accepted by the binary remains a hard
   FAIL via gate 1 — gate 1 is not weakened.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path


FLAG = re.compile(r"(?<![\w-])(--?[a-z][a-z0-9-]*)\b")
TABLE_HEADING = "## The CLI ("
# Top-level usage lines look like "  mischief plan      -f <exp.yaml> ...";
# require leading whitespace so the banner line ("mischief — fault injection
# ...", column 0) does not match, and require a verb-shaped token so the
# banner's em dash cannot be mistaken for a verb.
VERB_LINE = re.compile(r"^[ \t]+mischief\s+([a-z][a-z0-9-]*)\b")
SUPPORTED_LINE = re.compile(r"supported:\s*(\S.*)")
# Intentionally unknown flag used to elicit each verb's named refusal
# (MISCHIEF-CLI-001 shape: "… supported: --json, --version"). FlagSet-driven
# verbs answer "flag provided but not defined" instead, so they contribute
# nothing here — their real flags are already listed by --help.
PROBE_FLAG = "--__mischief_checker_probe__"

# DOC-35 pending-table exception (documented allowlist): verbs that ship but
# whose table row is not yet documented land here as labeled WARNings so the
# committed tree stays green; every other missing verb is a hard FAIL. The
# checker prints a NOTE if a pending verb becomes documented while its entry
# is still here, so the allowlist cannot rot. (2026-11-18: DOC-31 landed the
# `land` row, so the entry was retired; the set is empty again.)
PENDING_TABLE_VERBS: set[str] = set()


def flag_name(flag: str) -> str:
    """Compare flag names without caring whether help prints one dash."""
    return flag.lstrip("-")


NESTED = {"matrix", "check", "finding", "parse-agent-id", "record", "selftest-verdict", "fold", "run", "list"}


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


def run_capture(binary: Path, args: list[str]) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [str(binary), *args],
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )


def help_flags(binary: Path, command: tuple[str, ...]) -> set[str]:
    result = run_capture(binary, [*command, "--help"])
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


def shipped_verbs(binary: Path) -> set[str]:
    """Verbs the binary itself lists in its top-level usage lines.

    `mischief --help` is the shipped operator surface; spawn targets that
    usage deliberately omits (e.g. `__owner`) are not operator verbs and are
    correctly outside this set.
    """
    result = run_capture(binary, ["--help"])
    if result.returncode:
        raise RuntimeError(f"--help exited {result.returncode}: {result.stdout.strip()}")
    verbs = {
        match.group(1)
        for line in result.stdout.splitlines()
        if (match := VERB_LINE.match(line))
    }
    if not verbs:
        raise ValueError("no '  mischief <verb>' usage lines found in --help output")
    return verbs


def probe_flags(binary: Path, command: tuple[str, ...]) -> set[str]:
    """Flags the binary accepts beyond its ``--help`` list (empty if inconclusive).

    Gate 3 of this checker (DOC-35): drive ``<verb> --PROBE`` and parse the
    named refusal "supported: --a, --b". Only verbs that parse their own
    tokens answer that way; FlagSet-driven verbs answer "flag provided but
    not defined" (no supported list), and any other outcome is inconclusive,
    so all of those contribute an empty set.
    """
    result = run_capture(binary, [*command, PROBE_FLAG])
    if result.returncode != 2:
        return set()
    for line in result.stdout.splitlines():
        match = SUPPORTED_LINE.search(line)
        if match:
            return {flag_name(flag) for flag in FLAG.findall(match.group(1))}
    return set()


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
    warnings = 0

    # Gate 2: verb coverage — every shipped verb must have a table row.
    shipped = shipped_verbs(binary)
    documented_verbs = {command[0] for command in expected}
    missing_verbs = sorted(shipped - documented_verbs)
    hard_missing = [verb for verb in missing_verbs if verb not in PENDING_TABLE_VERBS]
    if hard_missing:
        failures += 1
        print(f"FAIL verb-coverage — shipped verb(s) with no table row: {' '.join(hard_missing)}")
    for verb in (verb for verb in missing_verbs if verb in PENDING_TABLE_VERBS):
        warnings += 1
        print(
            f"WARN verb-coverage — '{verb}' ships but has no table row"
            " (PENDING_TABLE_VERBS exception: row pending via DOC-31, not a failure)"
        )
    # Keep the exception honest: once the row lands the entry must go.
    for verb in sorted(PENDING_TABLE_VERBS & documented_verbs):
        print(f"NOTE verb-coverage — '{verb}' is documented in the table now; remove it from PENDING_TABLE_VERBS")

    # Gate 1 (unchanged comparison) + gate 3 flag-gap warning per command.
    for command, documented in expected.items():
        actual = help_flags(binary, command)
        # Gate 1's "documented but rejected" direction must test what the
        # binary truly accepts: a token-parsing verb (`version`) lists no
        # flags in its --help output, so without the probe a DOCUMENTED flag
        # would hard-fail as "not accepted by binary" even though the binary
        # accepts it (version --json exits 0, MISCHIEF-DF-72). FlagSet-driven
        # verbs probe empty, so this changes nothing for them — the gate is
        # made more accurate, not weaker (a documented flag the binary really
        # refuses still fails, via the same probe evidence).
        accepted = actual | probe_flags(binary, command)
        missing = sorted(actual - documented)
        extra = sorted(documented - accepted)
        gap = sorted((probe_flags(binary, command) - actual) - documented)
        label = " ".join(command)
        if missing or extra:
            failures += 1
            print(f"FAIL {label}")
            if missing:
                print("  missing from skill: " + " ".join(missing))
            if extra:
                print("  not accepted by binary: " + " ".join(extra))
        elif gap:
            warnings += 1
            print(
                f"WARN {label} — binary accepts undocumented flag(s): {' '.join(gap)}"
                " (flag-gap warning, exit 0: the verb's --help does not list them;"
                " discovered by probing with an unknown flag — the table must catch up)"
            )
        else:
            print(f"PASS {label} ({len(actual)} flags)")

    status = "FAIL" if failures else "PASS"
    print(
        f"cli-table: {status} — {len(expected)} command(s), {failures} mismatch(es),"
        f" {warnings} warning(s)"
    )
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
