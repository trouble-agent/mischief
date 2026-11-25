# mischief docs audit — tick mischief-perf-2026-11-25-17-57-24, delivery 8

Lane: mischief-docs (skill-driven documentation audit). Target: /home/kara/mischief.
Published tip audited: 959f2339a6d1e1b8f58b98876d459cbe97facf54 (origin/main == HEAD, fetch
`0 0`, `git ls-remote` agrees, checkout clean before this delivery's writes).

## What this delivery did

1. Re-verified the four still-pending doc rows filed by delivery 2 and re-checked by deliveries
   3, 6 and 7. All four premises reproduce unchanged at 959f2339: DOC-17-R2-R2 (env inventory
   13 tokens, MISCHIEFD_TEST_OWNER_CHILD_DIR 0 hits in docs/reference/operator.md, control
   MISCHIEF_REVERTER_SYSTEMD 1 hit, README.md:50 overclaim present), DOC-34-R2 (6 reverter
   record types, 0 reader-corpus hits for reverter.jsonl, 1 hit in the usage skill — a separate
   audience), DOC-11-R2 (scheduler DB `mischief%` rows = 11, docs/LANES.md:4 and README.md:52
   claim 21), DOC-16-R2 (0 PRD mentions of arm/land/profile run/profile list; §6.1 heading at
   docs/prd/mischief-v0.1.md:155; README.md:146 pointer present).
2. Found and filed ONE new gap the earlier deliveries did not own: the shipped CLI table still
   omits `version --version`, so the repo's own checker warns on the version verb. New row
   DOC-20-R2-R2 (P4, recurrence of the completed DOC-20-R2 whose acceptance demanded
   "PASS version (2 flags)" and "0 warning(s)").
3. Published delivery 7's verification report, which had been left stand-in-only: the file
   `audit-recheck-delivery7.md` in this directory is a byte-identical copy
   (sha256 dae37d8d937168fd91246bfd0a73f6de1bc275e19de6fbffed5c5329bc4e0824 before and after,
   `cmp` clean). Delivery 7 committed nothing and published nothing; this delivery publishes its
   report and states that gap rather than treating delivery 8 as the first re-check.

## The new finding, with the exact measurements

    $ grep -n '^version' skills/mischief-usage/SKILL.md
    50:version                      [--json]                              # stamped git sha ...; --json emits {"version":..,"tool":"mischief"}
    $ ./bin/mischief version --json     -> {"version":"959f2339a6d1","tool":"mischief"}   rc=0
    $ ./bin/mischief version --version  -> mischief 959f2339a6d1                          rc=0
    $ ./bin/mischief version --help     -> mischief 959f2339a6d1                          rc=0   (no flag list at all)
    $ ./bin/mischief version --nope     -> version: unknown flag --nope; supported: --json, --version   rc=2
    $ python3 tools/check_cli_table.py
    WARN version — binary accepts undocumented flag(s): version (flag-gap warning, exit 0: the verb's --help does not list them; discovered by probing with an unknown flag — the table must catch up)
    cli-table: PASS — 24 command(s), 0 mismatch(es), 1 warning(s)          rc=0

`make docs-check` ends with the same WARN line (its cli-table step). The founder fix (f599504)
added only `[--json]`; `--version` remains accepted, executable, and undocumented in the table,
and the verb's own `--help` prints no flag list, so the table is the only surface that can
document it. Because `version --help` answers with the version line, checker gate 1 sees zero
help-derived flags; the fix is the table row, not the checker (its gate-3 docstring forbids an
allowlist for exactly this).

## Gate state at the audited tip (unchanged by this delivery)

    $ make docs-check  ->  cli-table: PASS — 24 command(s), 0 mismatch(es), 1 warning(s); rc=0
    $ boardctl -C /home/kara/mischief validate  ->  RESULT: OK (267 warning(s))
    $ git show origin/main:.coding-hermes/board/events.jsonl | grep -c <tick>  ->  1  (event 576; no new event appended)

Marker-integrity A/B, run in a pristine detached worktree at origin/main so the environment is
identical: checker reports 47 violations there and 47 with this delivery's row and transcript
copied in — the new row adds ZERO violations. The 47 are pre-existing (legacy `/tmp/...`,
`git`, `commit` evidence values on MSF/README/DOC-6..8 rows) and are not this lane's repair.

## Boundary

Nothing was written to the target's README or docs; the only repo changes are the board row, its
`task_created` event, and the two audit artifacts in this directory. The four legacy rows were
re-verified, not amended.

ch:trace row=DOC-20-R2-R2 spec=none:docs-row wave=none:serial-lane-tick test=tools/check_cli_table.py doc=skills/mischief-usage/SKILL.md prompt=none:tick-body-not-retained evidence=.coding-hermes/audits/mischief-docs-2026-11-25-17-57-24/audit-transcript-delivery8.md witness=none:no-external-surface-observes-internal-cli-table-drift verdict=none:open-finding commit=none:open memory=none:board-row

## UNVERIFIED

- The four legacy findings were re-proved by static source-vs-doc differentials (T2), not by
  re-executing each documented path; their runtime proof is inherited from deliveries 2/3/6.
- `mischief version --help` printing the version instead of a usage block looks like a functional
  help-contract defect (the Go comment claims the conventional help contract); it was measured
  and quoted but NOT filed here — it is not a documentation-corpus gap and would need its own
  functional row.
- Whether any other checker/consumer reads the CLI table beyond tools/check_cli_table.py was not
  measured.
