# Serve / watchdog integration report — 2026-10-16 (dogfood tick, serve-daemon angle)

Target: `mischief` @ HEAD `1b53e52` (bin built from `1b53e52`; product code identical to
`bf74179` — the only delta since the previous install proof is `.coding-hermes/dogfood-log.md`).
Lane: `mischief-dogfood-2026-10-16-12-15-43`. Sibling run in the same tick: `dd6bcb8`
("run 15 — first drive of the arm verb (DF-05 fix)", filed `MISCHIEF-DF-29`); this report is the
second angle of the same tick and deliberately does **not** re-sweep the arm surface.

## Promise being tested

README, front door: *"Declare a fault, land it in a named target, prove it landed, **hold it under a
TTL owned by a reverting watchdog that outlives the CLI**, and get a verdict."*
README §"Arming a hold": the `arm` verb records the write-ahead half; **"The serve daemon (or a
detached TTL owner) reverts the hold at TTL expiry from the journal bytes alone"**;
`mischief revert --all` is the kill switch. PRD `docs/prd/mischief-v0.1.md:174`: the v0.1
`serve` daemon **"owns TTLs"**, reconciles leftovers at boot, serves `health.json`.

## What was actually driven (all in `/tmp/df-mc-r15`, run dir `/tmp/df-mc-r15/run`)

Sanction for the dev host (L1 container recipe, README §"First sanctioned run"):
`MISCHIEF_SANCTION=1` + `MISCHIEF_SANCTION_FILE=<home marker with a reason line>`.
Host loadavg during the run: 16.7 → 33.7 (above the AC-8 gate of 12) — see perf note.

| leg | command (abridged) | observed |
|---|---|---|
| arm (cold) | `mischief arm --fault S-001 --target scratch:/tmp/df-mc-r15 --ttl 25s --inverse-kind restore-file --inverse-params path=…/config.toml,backup=…/config.toml.bak --check-kind file-matches-backup --check-params … --dir …/run` | `armed 808fd9828ab37ff5`, write-ahead line, rc=0, wall 0.00s |
| status | `mischief status --dir …/run` | `holds: 1 … armed … ttl_remaining=24s`, `boot_id: 768b0e1c88fa3a76` |
| watchdog (daemon up before the arm) | `mischief serve --dir …/run`, fault then landed by corrupting the protected file | reverted at TTL **with no CLI process alive**; file byte-restored (`cfg=v1`); `health.json` `ok:true`, `holds[0].status=reverted`; proof row `reason=ttl_expiry detail=check: same duration≈178µs` |
| boot reconcile | arm → kill the daemon before TTL → wait past expiry → restart `serve` | fault stayed live while no daemon ran (`cfg=CORRUPTED-AGAIN`); the **fresh** daemon reverted it within 1s of start (proof ts 1792154032 vs started_at 1792154031, `own_pid` = the new daemon) |
| kill switch | `mischief revert --all --dir …/run` with a 600s hold armed | `kill switch: 2 holds reverted, wall=33ms (AC-6 budget 2s)`, rc=0, file restored |
| failure ladder (arm-time) | arm with a **deleted** backup | REFUSED rc=1 `arm: reverter: arm: inverse restore-file backup is not readable: …` — nothing written (DF-23 fix holds through the verb) |
| failure ladder (mid-hold) | arm OK → `rm` the backup → fault landed → TTL | honest `revert_failed`: `inverse failed: read backup: … no such file or directory; check: unreadable`; fault left **live**; no false green (EVT-1016) |
| **the break** | daemon **already running** (`pid 3212824`) → `arm --ttl 6s` (hold `2066303f`) → fault landed | **never reverted**: at T+26s / T+56s / T+86s `status` reads `armed … ttl_remaining=-15s / -55s / -1m30s`, file still `cfgD=FAULT-LANDED`; `health.json` **content** frozen at the daemon's boot snapshot (2 of 5 holds, `ok:true`, `revert_failed:0`) while its mtime advanced every 5s (the heartbeat is alive — the view is stale, not the process) |

## The defect, stated plainly (filed as `MISCHIEF-DF-30`, P1)

`serve` builds its lifecycle **once**, in `internal/daemon/daemon.go` `New()` (`lf: reverter.New(store)`);
`revertExpired()` (:134-146) iterates `d.lf.Status().Holds`, and `render()` (:177-186) folds the same
snapshot. `grep -rn 'func .*Reload' internal/` is empty — there is no reload API to call. Holds
appended to the journal by any process **after** the daemon started are therefore invisible to it:
never reverted at TTL, absent from `health.json`, uncounted in `reverted`/`revert_failed`.

The contrast that makes this a defect and not a design choice: the **CLI folds fresh**. In the same
run dir, `mischief status` listed all 5 holds, `mischief revert --all` acted on the post-boot holds,
and a **restarted** daemon reverted the same hold within a second and reported `ok:false` — i.e. the
machinery, the journal bytes and the arm records were all correct; only a long-running daemon's view
is stale.

Operator consequence: the natural deployment (watchdog as a service, `arm` per fault) silently loses
TTL containment — the fault stays landed past its TTL while the one document meant to flip green→red
reports `ok:true`.

## Right way to use it today (workaround until DF-30 lands)

1. Arm first, then start the watchdog — the documented quickstart order is safe:
   `mischief arm … && mischief serve --dir D`.
2. If a watchdog is already running and you need a new hold, **restart `serve` after arming**
   (boot reconcile folds the journal and reverts anything already expired), or use the kill switch.
3. `mischief status` and `mischief revert --all` **do** fold fresh — trust them over `health.json`
   for "is anything armed past its TTL?", and read `health.json` only for the daemon's own view.
4. `MISCHIEF_SANCTION=1` + a reason-bearing `MISCHIEF_SANCTION_FILE` marker is the working recipe on
   any host whose `/etc` is not writable.

## Naming trap (filed as `MISCHIEF-DF-32`, P3)

`health.json`'s `daemon_boot` is the **host-boot-scoped** reverter id
(`internal/reverter/journal.go:60-95` adopts the marker while `HostBoot == host`), so it is
byte-identical across three distinct daemon processes (pids `3178641` → `3212824` → `3386220`,
`started_at` differing) while `daemon.go:51` calls it "the daemon's own boot id". Freshness is
recoverable from `pid`/`started_at`/`generated_at`; a consumer keying on `daemon_boot` alone cannot
tell a dead daemon's document from the live one. `status` prints the same value under the bare
label `boot_id`.

## Unreachable door (filed as `MISCHIEF-DF-31`, P2)

`arm` prints "…serve (or Detach) reverts at TTL" (`cmd/mischief/arm.go:108`), but
`Lifecycle.Detach` (`internal/reverter/lifecycle.go:290`) has **zero production callers** — the
`__owner` entry exists (`cmd/mischief/main.go:105`) and the systemd-run/setsid shapes are
implemented (`internal/reverter/owner.go`), yet no verb or flag reaches it. With DF-30 in play, both
containment routes named to the operator are compromised (serve's view is stale; Detach is
uninvokable), leaving manual `revert --all` or a restart.

## Perf (Step 2b — measured, nothing filed)

| operation | measurement |
|---|---|
| `arm` cold (freshly built binary, first invocation) | `time -p`: wall 0.00s, user 0.00s, sys 0.00s |
| `arm` warm | `hyperfine --warmup 3 --runs 20 arm-warm-r15.sh`: **19.0 ms ± 6.2 ms** (user 2.3 ms, sys 5.2 ms — process startup) |
| TTL revert (measured act) | journal `duration_nanos` 178 340 ns / 254 641 ns — **sub-millisecond** |
| boot reconcile after restart | revert proof ts 1792154032 vs daemon `started_at` 1792154031 — **≤1 s** |
| kill switch (`revert --all`, 2 holds) | `wall=33ms` (AC-6 budget 2s) |
| `health.json` heartbeat | 5.00 s cadence (mtime 1792153877 → 1792153882) |

None of these make a user wait; the only waits in the whole loop are the deliberate TTL and the
one-time build. **No PERF row filed, deliberately.** The load gate does not block `arm`, `status`,
`serve` or `revert --all` at loadavg 16-34 — correct: those paths land nothing, and the kill switch
must never be gated.

## Install leg

**SKIPPED-install-bunker, explicit with citation.** The previous tick proved the same install premise
from scratch on `bunker-mvp` (evidence `/tmp/mischief-plane-evidence-20261016T080824Z-2492997.jsonl`,
agent `91b639bc`, all 8 steps pass: preflight/build/spawn/ship×2/collect/selftest/destroy; collect
journal 38 lines sha256-verified, landed=13 reverted=13 skipped=9; selftest 13 pass / 9 skip /
0 fail). `git log bf74179..HEAD --name-only` = `.coding-hermes/dogfood-log.md` only, so there is no
product delta; a fresh box would add nothing this run could not already cite.
