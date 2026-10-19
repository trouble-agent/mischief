# Dogfood run 20 — the TARGETED kill switch (`revert --hold <id>`), 2026-10-19

Lane: `mischief-chaos-dogfood` (tick `mischief-chaos-dogfood-2026-10-19-02-33-52`).
Product HEAD: `878c521` (pristine clone `git clone /home/kara/mischief /tmp/df-mc-r20/src` — the four
uncommitted DF-39 files in the live worktree are deliberately NOT part of this run's evidence).
Toolchain: swiss-army `go1.26.9`; binary = the clean-HEAD build (`CGO_ENABLED=0 linux/amd64`, 12,110,451 B,
`version` = `mischief unknown` because a clone stamps the sha at `make bin` time).
Sanction: every fault-landing leg ran with `MISCHIEF_SANCTION=1` and all state in `/tmp` run dirs.

## Why this surface

Delta check first. HEAD `878c521` **is** the previous tick's own commit; from it back through `2d1ed37`
every commit is board/doc-only (`git diff --stat 2d1ed37 878c521` = tasks.jsonl, dogfood-log.md,
docs/dogfood/*.md), no sibling row or event had landed since, and the only dirty entries are the same four
DF-39 files that have been uncommitted since 2026-10-17. So: nothing to re-prove, nothing to re-file.

`grep '.coding-hermes/dogfood-log.md'` over all 19 prior runs, before driving anything:

| string | hits |
|---|---|
| `revert --all` | 10 |
| `revert --hold` | **0** |
| `status --json` | **0** |
| `retention --apply` | 2 |

The kill switch had been exercised only in its `--all` form. The half a real operator reaches for when two
faults are held and one of them must come down now — `revert --hold <id>` — had never been driven.

## The promise being tested

From `README.md` ("Arming a hold") and the CLI table: `revert (--all | --hold <id>)` is the *forced revert
plus the measurement proving reverted*; `--all` is the kill switch over every live or stuck hold (AC-6,
`docs/prd/mischief-v0.1.md:382`); the product's whole thesis is "prove it landed, or it is a `no_op`" and
"revert is measured, not asserted".

## What was driven (scratch only, all legs re-run in this order)

Legs A–J, `/tmp/df-mc-r20/drive-{a,b}.sh`, run dirs `run`, `run-empty`:

* two holds armed (restore-file on `config.toml`, remove-file on `doomed.txt`), TTL 10m;
* the "landed fault" is simulated by hand — the CLI never lands a fault on this box (`--target scratch:…`);
* `revert --hold <first>` → `reverted c31f663363e7a483 outcome=reverted detail=check: same duration=0s`
  rc=0, the FIRST hold's file restored, the SECOND hold untouched (`doomed.txt` still present) and still
  `armed` in status; journal gains one `revert_proof` with `role=api reason=direct`;
* re-reverting the same hold → `no live hold for selector: … (already terminal: reverted)` rc=1;
* an id that never existed → same message without the terminal clause, rc=1;
* `--all --hold` together → `revert: --all and --hold are mutually exclusive` rc=2;
* neither flag → `revert: --all or --hold <id> is required (reverting nothing is a report, not a pass)` rc=2;
* the same hold id in a different `--dir` → rc=1 (the journal is the only source of truth);
* `revert --hold <second>` → rc=0, `doomed.txt` removed, `check: absent`.

Legs K–L, `drive-c.sh`: `arm` **refuses** a hold whose inverse cannot work (`inverse restore-file backup is
not readable`) rc=1 — a good guard worth keeping; a manual `revert --hold` on a live 3s-TTL hold, followed by
`serve` running 8s past the expiry, produced **no** second proof (`revert_proof` count stayed 1),
`health.json` `ok:true, reverted:1, revert_failed:0, stuck:0`, and the target file stayed restored. The
manual-revert ∪ TTL-owner overlap is clean.

Legs S1–S3 + AC-6, `drive-{d,e}.sh`: the failure and scale halves — this is where the four rows came from.

## Verified good (do not re-drive these)

* `revert --hold <id>` reverts exactly the named hold, measures the check, journals `role=api reason=direct`,
  and refuses every malformed invocation with a named message (rc=2 for usage, rc=1 for a missing/terminal
  hold). It ignores the load gate (worked at loadavg 23 while `battery`/`selftest` refuse) — correct: nothing
  lands.
* `arm` fails closed on an unreadable inverse backup *before* the hold is written.
* The reverter's terminal handling prevents a double revert when an operator reverts manually and the TTL
  owner wakes later.
* `serve` writes a coherent `health.json` (`ok`, per-hold status, `reverted`/`revert_failed`/`stuck` counts).

## The four findings (filed as rows)

| row | P | one line |
|---|---|---|
| `MISCHIEF-DF-51` | P2 | `revert --all` counts a FAILED revert as "reverted" in its summary line and always exits 0, while `revert --hold` exits 1 for the same outcome — a script gating on the kill switch reads a host with a live fault as clean |
| `MISCHIEF-DF-52` | P2 | a `revert_failed` hold is terminal and unreachable by every verb (`already terminal…`, `kill switch: 0 holds reverted` rc=0); the only recovery is re-arming a fresh hold |
| `MISCHIEF-DF-53` | P3 | `status --json` still ships PascalCase keys (`BootID`/`Holds`/`HoldID`…) while plan/doctor `--json` are snake_case (the DF-26 fix reached one verb only), and an unset `StuckAt` renders as `0001-01-01T00:00:00Z` |
| `MISCHIEF-DF-54` | P3 | `status` lists terminal holds (12 × `reverted` with a live-looking `ttl_remaining`) although its own contract says "active holds: armed or stuck", which makes AC-6's documented proof step ("`mischief status` empty") unsatisfiable |

## The right way (so the next agent does not pay for this again)

* Read the product's own reading surfaces back after an action: `status` for holds, `status --json` for a
  consumer's view, the run dir's `reverter.jsonl` for the raw record. They disagree in content *and* shape
  (DF-53/54), so never treat one as the other.
* To build a `revert_failed` hold fast, arm with an inverse that will succeed and a **check that cannot hold
  afterwards** (e.g. `--inverse-kind restore-file` + `--check-kind file-absent` on the restored path). An
  inverse that is broken *at arm time* is refused before it is journaled.
* Do not measure `revert --hold` with hyperfine over one arm: the hold id is content-derived, so the second
  run hits a terminal hold. Measure `revert --all` instead (or re-arm per run inside `--prepare`).
* `--all`-style verbs are the ones where honesty costs something: check the exit code *and* the summary line
  against the journal, not against the verdict words the verb chose.

## Perf (Step 2b) — nothing a user would notice, no PERF rows

| operation | number |
|---|---|
| `arm` (write-ahead) | 7.9 ms ± 0.4 (hyperfine, 20 runs) |
| `revert --all` over 12 holds | 5.0 ms ± 0.1 (10 runs) — the sweep's own wall is 53 ms per its summary line at N=12, vs the 2 s AC-6 budget |
| single targeted revert (in-proof duration) | 41–110 µs of work; whole process 5–8 ms |
| cold start (fresh inode copy, first invocation) | < 10 ms |

The measured cost of the surface is process startup. There is no slow path here to profile, so nothing was
profiled and nothing was filed.

## Install leg — PROVEN, not skipped

`scripts/bunker-plane.sh run --ttl=1h --server=bunker-mvp` from the pristine clone,
evidence `/tmp/df-mc-r20/plane-evidence.jsonl` — **8/8 rows pass**: preflight (bunker-mvp ONLINE),
build (`CGO_ENABLED=0 linux/amd64 mischief 878c5218e6bf`, 12,110,451 B), spawn (agent `06d0b3ec`, exec gate
passed), ship ×2 (repo frozen at HEAD `878c5218e6bf`, 277 tracked files; binary sha256-verified),
collect (journal 38 lines sha256-verified: landed=13 reverted=13 skipped=9), remote `selftest --all` exit 0
(13 pass, 9 skip, 0 fail of 22), destroy `TEARDOWN-GONE` (agent absent from the bunker list, zero residue).

## Read-only statement

No product code was touched: the drive is `/tmp` scripts plus a clone, the holds live in `/tmp` run dirs and
were never landed on a real target, no `/etc` or sudoers state was touched, and no pre-existing board row or
event was edited. Board writes: four new rows + four new events, appended line-wise with a pre-append
snapshot at `/tmp/df-mc-r20/board-snap/` and a prefix-sha assertion; `boardctl validate` → `RESULT: OK
(177 warning(s))`, the same count as the pre-append board.
