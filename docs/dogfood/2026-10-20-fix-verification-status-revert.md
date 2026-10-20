# Dogfood run 21 — re-proving the DF-52/53/54 fixes as a real operator, 2026-10-20

Lane: `mischief-dogfood` (tick `mischief-dogfood-2026-10-20-03-43-42`).
Product HEAD: `9ecc25d` (clean clone `git clone /home/kara/mischief /tmp/df-mc-r21/head` — the 12 MB
tracked `mischief` binary that is dirty in the live worktree is NOT this run's evidence).
Binary: `CGO_ENABLED=0 linux/amd64`, `make bin` 2.4 s warm, `version` = `mischief 9ecc25deca32`.
Sanction: every fault-landing verb ran with `MISCHIEF_SANCTION=1`; all state in `/tmp/df-mc-r21` run dirs;
no fault was landed on a real target (the "landed" state is simulated by editing the scratch target file).

## Why this surface

The foreman wave `mischief-foreman-2026-10-20-01-27-42` closed three dogfood rows the same night:
`MISCHIEF-DF-52` (`revert --retry`), `MISCHIEF-DF-53` (`status --json` snake_case + null timestamps) and
`MISCHIEF-DF-54` (`status` defaults to active holds, `--all` for the rest). All three were marked `done`
and CI on `main` was green (runs 39356645045 / 39357226848 / 39358193764 / 39358317514). A fix closing is
a claim, not evidence — the honest next use of the tool is the operator workflow those three fixes exist
for: arm a hold, land a fault, watch a revert FAIL, recover from it, and read the surface back.

## The promise being tested

`README.md` (§Arming a hold, §Safety): "revert is measured, not asserted", the kill switch is
`mischief revert --all`, `status` "folds the run dir's journal: armed/stuck holds + TTL remaining, proofs",
and (the product's thesis) a run that cannot show the fault arrived fails. An operator must be able to
tell a clean host from a host with a live fault, and recover a fault whose revert failed.

## What was driven

Legs A/B/C (`/tmp/df-mc-r21/legs-abc.sh`), B2/D (`legs-b2d.sh`), E (`legs-e.sh`); scratch
`/tmp/df-mc-r21/scratch*`.

* **DF-54, text leg — VERIFIED.** arm (15 s TTL) → `status` default `holds: 1 … ttl_remaining=14s` → land
  the fault by hand → `revert --all` rc=0 → `status` default `holds: 0` with the proof still listed (AC-6's
  "`mischief status` empty" proof step is now satisfiable) → `status --all` lists the terminal hold and
  prints **no** `ttl_remaining`.
* **DF-53 — VERIFIED.** `status --json` top level `[boot_id history holds]`, hold row
  `[expiry fault_id hold_id land_pid own_pid status stuck_at target]`, `stuck_at: null` while live,
  `jq '.holds[0].hold_id'` reads the id, and a `paths(scalars)` census finds **no** PascalCase key at any
  depth.
* **DF-51, exit-code half — VERIFIED.** one good + one doomed hold → `revert --all` prints
  `kill switch: 1 holds reverted, 1 revert_failed, wall=32ms (AC-6 budget 2s)`, exits **rc=1**, and stderr
  names the live-fault risk.
* **DF-52 — VERIFIED.** arm → sabotage the inverse's backup *after* arming → `revert --hold` rc=1
  `outcome=revert_failed` → a second plain revert refuses quoting `--retry` → heal the backup →
  `revert --hold <id> --retry` **rc=0**, `outcome=reverted`, target file byte-restored, and the journal
  carries all three proofs (direct/retry/retry) with their reasons. Admission is narrow: retry on a
  **reverted** hold and on an **armed** hold both refuse (`--retry requires a hold whose status is
  revert_failed`), journal sha256 byte-unchanged. `--all --retry` rc=2, bare `--retry` rc=2, unknown id rc=1.
* **DF-54's regression risk — CHECKED, CLEAN.** The active-only default could have silently stripped the
  daemon's failure signal. It did not: with a live `revert_failed` hold in the run dir,
  `serve` → `health.json {"ok":false,…,"revert_failed":1}`; after a successful `--retry`, a fresh boot →
  `{"ok":true,…,"reverted":1,"revert_failed":0}`. The daemon still folds the complete hold set.

## Findings (rows filed — see the board for the full reasoning)

1. **`MISCHIEF-DF-54-R2`** (P3, RECUR of DF-54). The active-only default reached the text line but not the
   honesty of the surface. With a fault **still landed** whose measured revert failed, `mischief status`
   prints `holds: 0` and exits **0** (the failed hold survives only as a proof line) while `health.json` for
   the **same run dir** says `ok:false / revert_failed:1` — CLI and daemon disagree about whether a fault may
   still be live, which is DF-51's premise one verb over. On `status --json --all` a terminal hold still
   publishes its arm-time `expiry` (measured **+120 s** on a hold reverted before its TTL — a positive
   countdown with no `active` marker), and the healthy state renders `"holds": null` rather than `[]`.
2. **`MISCHIEF-DF-51-R2`** (P4, RECUR of DF-51). The per-hold stdout line still leads with the success word
   for every outcome: `reverted 6027df6c6e36e50d outcome=revert_failed fault=S-001 detail=…`
   (`cmd/mischief/main.go:365`, same fixed label at `:403`; the retry path at `:401` shows the correct
   pattern). The first word is what an on-call human or a log scraper reads.
3. **`MISCHIEF-DF-55`** (P4). Neither new flag is documented outside the usage text:
   `grep -rn -- '--retry' README.md docs/` and `grep -rn -- 'status --all' README.md docs/` return **zero**
   hits, so the only recovery out of the `revert_failed` dead end is discoverable by running the bare binary.

## The right way to do this next time

* Build the evidence from a **clean clone of HEAD**, never the live worktree (the tracked 12 MB binary is
  dirty there; MISCHIEF-RV-4 covers the tracking itself).
* To create a **failed revert** deliberately: arm with a `restore-file`/`file-matches-backup` pair, then
  move the backup away *after* arming (`arm` pre-checks readability, so the sabotage must come second).
  Heal by moving it back and `chmod 644` — mode 000 survives `mv` and yields `permission denied`, which
  looks like a product bug and is a probe bug.
* `status` default hides terminal holds by design; read `status --all` when you need to see a hold that
  failed, and read `health.json` (or the proofs list) when you need the failure *count*.

## Perf (Step 2b) — measured, nothing filed

`hyperfine` (warm) / `time -p`, host loadavg ~29 (loaded), binary = the HEAD build:

| operation | number |
|---|---|
| `status` (armed hold present) | 7.7 ms ± 5.2 (user 1.6 / sys 2.9) |
| `status --json --all` | 13.1 ms ± 6.0 |
| `arm` (fresh run dir, 20 runs) | median 10 ms (first two invocations 0.15 / 0.13 s — cold page cache) |
| `revert --all` (no holds) | 5.8 ms ± 0.9 |
| full recovery loop, end to end (arm + doomed revert + retry + recovered) | 0.06 s real (user 0.01 / sys 0.03) |
| `serve` cold boot → first `health.json` | 12–13 ms (3 trials) |

Everything is process-startup bound; the only multi-second cost on this surface is the one-time build
(2.4 s warm in this repo, 194 s on a fresh agent with a cold Go toolchain). **No PERF row filed: a user
cannot feel any of this**, and filing it would devalue the real rows.

## Install leg — PROVEN on a fresh machine, not skipped

`scripts/bunker-plane.sh run --ttl=1h --server=bunker-mvp` with `BUNKER_PLANE_REPO` pointed at the frozen
HEAD clone, evidence `/tmp/df-mc-r21/plane-evidence.jsonl` — **8/8 rows pass**: preflight (bunker-mvp
ONLINE), build (`CGO_ENABLED=0 linux/amd64 mischief 9ecc25deca32`, 12,138,994 B), spawn (agent `c949fc6e`,
exec gate passed), ship ×2 (repo frozen at HEAD, 296 tracked files, 7,515,251 B tar; binary sha256-verified
on arrival), collect (journal 38 lines sha256-verified, landed=13 reverted=13 skipped=9), remote
`selftest --all` rc=0 (13 pass / 9 skip / 0 fail of 22), destroy `TEARDOWN-GONE` (agent absent from the
bunker list, zero residue).

## Read-only statement

No product code was touched. The drive is `/tmp` scripts plus a clone; the holds live in `/tmp` run dirs and
no fault was landed on a real target; no `/etc`, sudoers or scheduler state was touched; no pre-existing
board row or event was edited. Board writes this run: three appended rows (`MISCHIEF-DF-54-R2`,
`MISCHIEF-DF-51-R2`, `MISCHIEF-DF-55`), ids minted by `board_next_id.py` (two `--recur` of the closed base
rows, one fresh), `boardctl validate` → `RESULT: OK`.
