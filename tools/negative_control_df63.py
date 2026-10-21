#!/usr/bin/env python3
"""Negative scratch control for the MISCHIEF-DF-63 row-drift gate.

Builds a scratch copy of the repo, re-introduces ONE of the two stale
sentences verbatim (the pre-cdc6ee4 MISCHIEF-DF-60 wording), and requires
the checker to FAIL with the row id + status named.
"""
import shutil
import subprocess
import sys
from pathlib import Path

REPO = Path("/home/kara/worktrees/mischief-MISCHIEF-DF-63")
CTL = Path("/tmp/mc-df63-ctl")

# fresh scratch mirror of everything the checker reads
if CTL.exists():
    shutil.rmtree(CTL)
CTL.mkdir(parents=True)
shutil.copytree(REPO / "skills", CTL / "skills")
shutil.copytree(REPO / ".coding-hermes", CTL / ".coding-hermes")
(CTL / "tools").mkdir()
shutil.copy(REPO / "tools" / "check_row_drift.py", CTL / "tools" / "check_row_drift.py")
shutil.copy(REPO / "README.md", CTL / "README.md")

skill = CTL / "skills" / "mischief-usage" / "SKILL.md"
text = skill.read_text()
anchor = "- An **absent** run dir is a named refusal (MISCHIEF-DF-60, closed by d51d65e/72d3939; re-verified live"
stale = (
    "- An **absent** run dir reads as a clean sweep (MISCHIEF-DF-60): "
    "`revert --all --dir <nonexistent>` prints `kill switch: 0 holds reverted, "
    "wall=0s` rc=0 and the exit code means the host is clean."
)
assert anchor in text, "anchor sentence not found in scratch copy"
assert "reads as a clean sweep" not in text, "stale sentence already present?"
skill.write_text(text.replace(anchor, stale + "\n" + anchor, 1))

r = subprocess.run(
    ["python3", "tools/check_row_drift.py"], cwd=CTL, capture_output=True, text=True
)
print(r.stdout, end="")
print(f"NEGATIVE rc = {r.returncode}")
if r.returncode == 0:
    print("NEGATIVE CONTROL FAILED: checker did not bite on the stale sentence")
    sys.exit(1)
if "MISCHIEF-DF-60" not in r.stdout:
    print("NEGATIVE CONTROL FAILED: row id not named in the failure output")
    sys.exit(1)
print("NEGATIVE CONTROL PASSES: checker fails nonzero naming MISCHIEF-DF-60 + status")
