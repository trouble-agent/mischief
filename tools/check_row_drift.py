#!/usr/bin/env python3
"""Row-status drift gate (MISCHIEF-DF-63).

The repo's CURRENT-behaviour prose — skills/**/*.md pitfall bullets and README.md —
must not cite a CLOSED board row as if its defect were still live. This gate
resolves every `MISCHIEF-DF-<n>[-R<k>]` / `MSF-<n>` reference in that prose
against .coding-hermes/board/tasks.jsonl and FAILS, naming file:line, the row id
and the row's status, when a closed row is cited without row-status context
(a status word on the same or the following line).

Exempt by design:
  - docs/ and probe/ and .coding-hermes/ are not scanned: dated run reports,
    dogfood history and board rows are ALLOWED to record a row as open at the
    time of the run (AC-5).
  - `ch:trace` marker lines are metadata, not behaviour claims.
  - Row-id RANGES ("MSF-001..022") describe numbering, not behaviour.
  - An id absent from the board is reported as unresolved, never guessed.

Exit 0 = green, exit 1 = at least one stale current-behaviour claim.
"""

import json
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
BOARD = REPO / ".coding-hermes" / "board" / "tasks.jsonl"
SCAN_PATHS = [REPO / "skills", REPO / "README.md"]
EXEMPT_DIR_PARTS = ("docs/", ".coding-hermes/", "probe/")

# MISCHIEF-DF-58, MISCHIEF-DF-51-R2, MSF-024 — but not "MSF-001..022" ranges
# (a range is row numbering, not a behaviour claim) and not bare "DF-58".
ROW_ID = re.compile(r"(MISCHIEF-DF-\d+(?:-R\d+)?|MSF-\d+)(?!\.\.)")

CLOSED_STATUSES = {"complete", "done"}

# Row-status context that makes a closed-row citation honest: a status word on
# the id's line or the line right after it ("closed by f794ed0", "DF-30 is\nfixed",
# "arm NOW evaluates …"). Deliberately EXCLUDES bare "verified"/"live"/dates —
# the two drift bullets this gate exists for both carried "verified live <date>".
STATUS_CONTEXT = re.compile(
    r"""(?ix)
    (?<![\w-])
    (?:
        closed | fixed | resolved | superseded | landed | shipped | done | merged
      | decided | released | renamed | stale
      | now\s+(?:closed | fixed | evaluates | handles | validates | works
                | refuses | reports | carries | prints | reads | folds)
      | since\s+fixed
    )
    """
)


def load_statuses(board: Path) -> dict:
    statuses = {}
    with board.open() as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            row = json.loads(line)
            statuses[row["id"]] = row.get("status")
    return statuses


def scan_files() -> list:
    files = []
    for path in SCAN_PATHS:
        if path.is_file():
            files.append(path)
        elif path.is_dir():
            files.extend(sorted(p for p in path.rglob("*.md") if p.is_file()))
    out = []
    for p in files:
        rel = p.relative_to(REPO).as_posix()
        if any(part in rel + "/" for part in EXEMPT_DIR_PARTS):
            continue
        out.append(p)
    return out


def check_file(path: Path, statuses: dict):
    failures = []
    lines = path.read_text().splitlines()
    for i, line in enumerate(lines):
        if line.lstrip().startswith("ch:trace") or "ch:trace row=" in line:
            continue  # marker metadata, not a behaviour claim
        # ranges ("MSF-001..022", incl. bare-digit endpoints) describe row numbering, not behaviour
        scan = re.sub(r"(?:MISCHIEF-DF-\d+(?:-R\d+)?|MSF-\d+|)\.\.(?:MISCHIEF-DF-\d+(?:-R\d+)?|MSF-\d+|)", "", line)
        for m in ROW_ID.finditer(scan):
            row_id = m.group(1)
            status = statuses.get(row_id)
            if status is None:
                continue  # unresolved ids are reported separately, never guessed
            if status not in CLOSED_STATUSES:
                continue  # pending/in_progress rows may be cited as live
            # The following line may rescue only if it is a CONTINUATION of this
            # bullet (indented). A col-0 "- " line is a new bullet: without this
            # rule a neighbour's "closed by ..." bleeds into an adjacent stale
            # claim and exempts it (caught by the negative control).
            nxt = ""
            if i + 1 < len(lines) and lines[i + 1][:1] in (" ", "\t"):
                nxt = "\n" + lines[i + 1]
            window = line + nxt
            if not STATUS_CONTEXT.search(window):
                failures.append(
                    f"{path.relative_to(REPO)}:{i + 1}: closed row {row_id} "
                    f"(status={status}) cited without row-status context: "
                    f"{line.strip()[:140]}"
                )
    return failures


def main() -> int:
    statuses = load_statuses(BOARD)
    files = scan_files()
    failures = []
    resolved = 0
    unresolved = []
    for path in files:
        for i, line in enumerate(path.read_text().splitlines()):
            # same range-stripping as the fail path, so the census does not
            # report range endpoints ("MSF-001..022" -> phantom "MSF-00")
            scan = re.sub(r"(?:MISCHIEF-DF-\d+(?:-R\d+)?|MSF-\d+|)\.\.(?:MISCHIEF-DF-\d+(?:-R\d+)?|MSF-\d+|)", "", line)
            for m in ROW_ID.finditer(scan):
                row_id = m.group(1)
                if row_id in statuses:
                    resolved += 1
                else:
                    unresolved.append(f"{path.relative_to(REPO)}:{i + 1}: {row_id}")
        failures.extend(check_file(path, statuses))

    print(
        f"row-drift: scanned {len(files)} prose file(s), "
        f"resolved {resolved} row reference(s) against {len(statuses)} board rows"
    )
    if unresolved:
        print(f"row-drift: NOTE {len(unresolved)} reference(s) not found in board:")
        for u in unresolved:
            print(f"  ? {u}")
    for f in failures:
        print(f"FAIL {f}")
    if failures:
        print(f"row-drift: FAIL — {len(failures)} closed row(s) cited as current behaviour")
    else:
        print("row-drift: PASS — no closed row cited as current behaviour")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
