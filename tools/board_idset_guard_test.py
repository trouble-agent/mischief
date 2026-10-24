#!/usr/bin/env python3
"""Regression tests for the staged-board id-set guard."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


REPO = Path(__file__).resolve().parents[1]
GUARD = REPO / "tools" / "board_idset_guard.py"
BOARD = ".coding-hermes/board/tasks.jsonl"


def row(row_id: str, status: str = "pending") -> str:
    return json.dumps({"id": row_id, "status": status}, separators=(",", ":"))


class BoardIDSetGuardTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.repo = Path(self.tmp.name)
        (self.repo / ".coding-hermes/board").mkdir(parents=True)
        self.run_git("init", "-q")
        self.run_git("config", "user.email", "test@example.invalid")
        self.run_git("config", "user.name", "Board Guard Test")
        self.write_board([row("KEEP-1"), row("KEEP-2")])
        self.run_git("add", BOARD)
        self.run_git("commit", "-qm", "parent board")

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def run_git(self, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["git", *args], cwd=self.repo, text=True, capture_output=True, check=True
        )

    def write_board(self, rows: list[str]) -> None:
        path = self.repo / BOARD
        path.write_text("\n".join(rows) + "\n")

    def stage_rows(self, rows: list[str]) -> None:
        self.write_board(rows)
        self.run_git("add", BOARD)

    def run_guard(self, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(GUARD), BOARD, *args],
            cwd=self.repo,
            text=True,
            capture_output=True,
            env={**os.environ, "GIT_CONFIG_GLOBAL": "/dev/null"},
        )

    def test_dropped_id_fails_and_names_the_id(self) -> None:
        self.stage_rows([row("KEEP-1")])

        result = self.run_guard()

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("dropped ids", result.stderr)
        self.assertIn("KEEP-2", result.stderr)

    def test_pure_append_is_allowed(self) -> None:
        self.stage_rows([row("KEEP-1"), row("KEEP-2"), row("NEW-1")])

        result = self.run_guard()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("PASS", result.stdout)

    def test_status_only_edit_is_allowed(self) -> None:
        self.stage_rows([row("KEEP-1", "complete"), row("KEEP-2")])

        result = self.run_guard()

        self.assertEqual(result.returncode, 0, result.stderr)

    def test_explicit_acknowledges_dropped_id(self) -> None:
        self.stage_rows([row("KEEP-1")])

        result = self.run_guard("--ack-dropped")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("ACKNOWLEDGED", result.stdout)
        self.assertIn("KEEP-2", result.stdout)

    def test_compares_index_not_working_tree(self) -> None:
        self.stage_rows([row("KEEP-1"), row("KEEP-2"), row("NEW-1")])
        # Deliberately make the unstaged working tree look destructive after
        # staging. The commit candidate remains the staged append and must pass.
        self.write_board([row("KEEP-1")])

        result = self.run_guard()

        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
