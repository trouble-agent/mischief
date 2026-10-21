# Dogfood tick 25 — the reverter recurrence claim, re-checked (2026-10-20)

Lane `mischief-chaos-dogfood`, tick `mischief-chaos-dogfood-2026-10-21-02-36-57`,
HEAD `5233f4f43387cd8009af596e5316a3e5d2a49844`, product built as a clean
`git archive HEAD` export (`make bin SHA=5233f4f` → `mischief 5233f4f`).
All state under `/tmp/df-mc-r25`; no product source, board row, `/etc`, scheduler
state or credential touched.

## Why this angle (and why nothing else)

The previous dogfood tick (24th, `cdc6ee4`) filed MISCHIEF-DF-61/62/63. Since then the
only commit is `5233f4f` — `git show --stat` = `.coding-hermes/board/events.jsonl` (+1),
i.e. **zero product delta**. What *did* change is a QA battery event: event 207 recorded
a **ci-pass FAIL** on `TestOwnerSurvivesKill9AndRevertsAtTTL` at battery head `cdc6ee4`
(agent `f60a3c73`, bunker-mvp, `lifecycle_test.go:635 — timeout after cli reaped`), plus
the same failure in the 3 GiB chaos-resource cell.

So the change since the last tick is a *claim about the current code*, and the honest
response is to re-check that claim rather than re-sweep a covered surface. Every other
candidate surface was already driven by earlier ticks — `retention`, `install`/`uninstall`
apply legs, the released tarball consumer path, `arm`/`revert`/protected rails, `serve`,
`battery`, `chaos`, `plane`, `audit`, `status --json` — so none was re-run.

## Promise under test

*"A fault is held under a TTL owned by a reverting watchdog that outlives the CLI: the
fault is still reverted at TTL expiry after the CLI is killed — measured, journaled, with
an owner pid that is neither the CLI's nor the test's."* A red cell is a claim, not proof;
a green local run is a claim too. Both were driven.

## Leg A — the named test, on the dev box (`logs/native-summary.txt`)

| arm | command | result |
|---|---|---|
| isolated ×3 | `go test ./internal/reverter/ -run TestOwnerSurvivesKill9AndRevertsAtTTL -count=1 -v` | **rc=0** ×3, 5.46 s / 5.98 s / 5.97 s (loadavg 11.5–12.0) |
| whole package | `go test ./internal/reverter/ -count=1` | **rc=0** (12.25 s) |
| 3 GiB cap (QA's chaos-resource cell shape) | `systemd-run --user --scope -p MemoryMax=3145728K <named test>` | **rc=0** (5.55 s) |
| **saturation arm** (not run by QA) | 16 `nice -n 19` spinners for 45 s, named test ×2 | **rc=0** (6.16 s, 5.93 s) at loadavg 19.9–21.0, peak 23.6 |

7/7 green, including at load well above the repo's own load-gate threshold (12). The
saturation arm is the discriminating one: if the failure were a load/timing margin, a
host at load 21 should show it. It did not.

This agrees with the QA lane's own off-battery legs already recorded on QA-MISCHIEF-005
(isolated 10/10, full suite, `ulimit -v` suite, netcut suite, hosted CI green at the exact
battery head) and with the 23rd dogfood tick's non-repro at `337ebe5`. The failure stays
**agent-side; mechanism still unverified** — which is precisely what QA-MISCHIEF-002-R2
says, and why this tick files **no row**: the premise is owned.

**Hypothesis formed, measured, and rejected.** The natural explanation — "the test waits
only 10 s after the kill while the product's watchdog can revert ~5 s late" — is *wrong for
this test*. The AC-5 test measures the CLI's **detached owner**, and that path reverted
inside the window in every run (whole test 5.5–6.2 s wall). The slow path is the fold-driven
`serve` watchdog, measured in Leg B. Filing the window theory as a finding would have put a
row in front of the foreman that the very next measurement disproves.

## Leg B — the TTL-owner containment point at TTL=6 s (`logs/ttl6b-summary.txt`)

Documented operator path, six times: `arm --fault S-001 --target scratch:<dir> --ttl 6s
--inverse-kind restore-file … --dir <dir>`, `serve --dir <dir>`, then LAND by hand
(`printf FAULTED-CONTENT > target.live`) and poll the target's **own bytes** every 50 ms.

| run | restored | revert after LAND |
|---|---|---|
| 1–6 | yes 6/6 | 9.67 / 9.55 / 9.66 / 9.64 / 9.62 / 9.63 s (mean **9.63 s** for a declared 6 s TTL) |

That is +3.6 s late — inside the ~5 s fold tick already named by **MISCHIEF-DF-59**, and a
third independent sample at a TTL that row had not measured (it had 10 s and 5 s). New and
useful for the fix: the lateness is a property of the **fold loop**, not of TTL ownership in
general — the detached owner the test exercises reverted on time. Recorded as an event on
DF-59, not as a new row.

## Leg C — install leg, PROVEN (not skipped)

`scripts/bunker-plane.sh run --ttl=1h --server=bunker-mvp`, frozen HEAD `5233f4f`:

* PLANE pass, rc=0; evidence `/tmp/mischief-plane-evidence-20261021T030252Z-1625517.jsonl`
  — **8/8 evidence rows `status: pass`** (preflight, build, spawn, ship ×2, collect, selftest, destroy)
* agent `0f5cf698`, destroy TEARDOWN-GONE, absent from the bunker list afterwards
* build `CGO_ENABLED=0 linux/amd64` 12,168,697 B stamped `5233f4f43387`
* remote selftest: `journal=38 lines landed=13 reverted=13 skipped=9 rc=0`

## Perf (Step 2b) — measured, nothing filed

| operation | number |
|---|---|
| named test, isolated (warm) | 5.46 / 5.98 / 5.97 s; package 12.25 s |
| named test under saturation (loadavg ~20) | 6.16 / 5.93 s |
| `serve`-path containment point, TTL 6 s | 9.63 s mean after LAND (n=6, spread 0.12 s) |
| plane install leg end-to-end (build → destroy) | ~90 s wall, dominated by agent spawn + remote selftest |

The only wait a human feels here is the TTL/fold-tick lateness — already owned by DF-59 and
re-evidenced above. No PERF row: the verbs themselves are ~4 ms process-startup bound
(prior ticks' hyperfine sets stand, product code unchanged).

## Probe artifacts recorded, not filed

* First TTL leg ran the binary from `$HOME` and `arm` refused:
  `arm: catalog/faults not found from cwd upward` — the binary resolves the catalog from the
  cwd upward, `arm --help` documents `-catalog-dir`, and the 2 runs it produced
  (`restored=no`, 22 s) were the *unarmed* state, not a product defect. Re-run from the
  checkout root: 6/6 restored. Disclosed so the next tick does not chase it.
* No arm of this re-check ran on a bunker agent (a there-leg needs a Go toolchain on the
  agent); the agent leg proves the install plane only. That limit is written into the event.

## Verdict

**FAILED-product for the QA claim on this host — i.e. the claim does not hold here; PASS-run.**
The reverter red is not reproducible on the dev box (7/7, including a saturated arm) and the
one mounting explanation (a too-narrow test window) was measured and rejected; the failure
stays environment-side with the mechanism unverified, exactly as QA-MISCHIEF-002-R2 records.
The install plane is proven green at HEAD, and the containment-point lateness is re-evidenced
for the row that owns it.

## Artifacts

* board `events.jsonl` +2 rows: **event 208** on `QA-MISCHIEF-005` (re-check, saturation arm,
  rejected hypothesis, honest limit) and **event 209** on `MISCHIEF-DF-59` (TTL 6 s sample)
* this file; the canonical run log entry (`.coding-hermes/dogfood-log.md`)
* install-leg evidence `/tmp/mischief-plane-evidence-20261021T030252Z-1625517.jsonl`
* probe scratch `/tmp/df-mc-r25/**` (build, per-run logs, summaries)

**No new row was filed, deliberately** — both premises are owned by open rows.
Read-only lane: no product code touched, no pre-existing row/event edited, no fault landed on
a real target (scratch dirs only, LAND simulated by hand on a scratch file), no `/etc` or
sudoers state touched, no scheduler state touched, no foreman woken, no credential minted,
repo visibility/permissions untouched.
