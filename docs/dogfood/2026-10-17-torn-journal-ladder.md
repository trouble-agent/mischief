# The torn-journal ladder — what `serve` does when the watchdog's own journal
# is corrupt (dogfood run 18, 2026-10-17, HEAD c28800d)

Scope: this is a REAL-USE record, not a test report. Everything below was driven
as an operator would drive it — a scratch run dir, the documented commands, a
landed fault, the journal corrupted with one appended torn line — on the binary
built from HEAD (`c28800dee641`). Every command is reproducible; scratch root
was `/tmp/df-mc-r18`.

## Why this ladder exists

`mischief arm` writes a reverter hold into `<run>/reverter.jsonl` **fsynced
before the fault may land** (README §Arming a hold, SPEC-04). The `serve`
daemon then owns the TTL: it folds that journal, reverts at expiry, and writes
`health.json` so "a reader never needs to talk to the daemon" (internal/daemon
package doc). The whole safety story therefore rests on the daemon behaving
correctly **in the one state where the journal is damaged** — otherwise the
fault that was supposed to be contained is still live and nobody can see it.

Two dogfood runs ago (run 17) that state killed the daemon: a torn tail made
`Run()` return before `writeHealth`, so `health.json` froze at the last good
beat. The fix (251371d, MISCHIEF-DF-39) landed since. This run drove the fixed
daemon through the whole ladder and found that **the crash is gone, but the
degraded behaviour is now split into three separate defects** — one per rung.

## The setup (used by every rung)

```sh
BIN=/home/kara/mischief/bin/mischief          # built this run, stamp c28800dee641
D=/tmp/df-mc-r18/liveN ; mkdir -p $D/run $D/scratch
printf 'orig-line-1\n' > $D/scratch/config.toml
cp $D/scratch/config.toml $D/scratch/config.toml.bak
printf '%s\n' "sanctioned: dogfood r18 scratch" > $D/sanction
export MISCHIEF_SANCTION=1 MISCHIEF_SANCTION_FILE=$D/sanction
$BIN serve --dir $D/run &                     # rungs 2-4 start the daemon FIRST
$BIN arm --fault S-001 --target scratch:$D/scratch --ttl 120s \
  --inverse-kind restore-file \
  --inverse-params path=$D/scratch/config.toml,backup=$D/scratch/config.toml.bak \
  --check-kind file-matches-backup \
  --check-params path=$D/scratch/config.toml,backup=$D/scratch/config.toml.bak \
  --dir $D/run                                # rc=0, hold journaled
printf 'FAULTED-by-dogfood-r18\n' > $D/scratch/config.toml     # LAND the fault
printf '{"type":"arm","hold_id":"torn-tail' >> $D/run/reverter.jsonl   # TEAR it
```

Note for anyone repeating this: the sanction variable is `MISCHIEF_SANCTION`
(no "F") — a transposed `MISFCHIEF_SANCTION=1` produces the *same*
`host_not_sanctioned` refusal as setting nothing at all, so check the spelling
before believing the gate is broken.

## Rung 0 — the happy path still holds (control)

`serve` first, then `arm`, then land, then let the TTL pass: the running daemon
re-folds the journal every 5 s beat (`HeartbeatInterval`), reverted the hold at
expiry with `reason=ttl_expiry detail='check: same'`, byte-restored the target
and folded it into `health.json` (`holds:1 reverted:1`). That is the DF-30 fix
still holding, and it is the baseline the rungs below are measured against.

## Rung 1 — a torn line appended WHILE the daemon runs: daemon survives,
## health lies, containment stalls  → MISCHIEF-DF-39-R2 (P1)

Reproduced three times (`live`, `live2`, `live3`), including once with the fault
landed:

| observation (leg3, T+40 s, TTL expired 20 s earlier) | value |
|---|---|
| daemon process | alive (`goroutine 1 [select]` at `daemon.go:143` — the loop IS ticking) |
| `health.json` `ok` | `true` |
| `health.json` `status` of the hold | `armed` |
| `health.json` `refold_error` | absent |
| `health.json` staleness at the read | 43 s (heartbeat is 5 s) |
| target file | still `FAULTED-by-dogfood-r18-leg3` |
| revert proofs in journal | 0 |

Cause: on the tick, `revertExpired()` returns the re-fold error, the loop calls
`setRefoldErr(err)` and then `continue` — **skipping the `writeHealth()` that
follows it** (`internal/daemon/daemon.go:148-160`, the skip is the `continue` at
:157). Every later tick takes the same branch, so the document is never
rewritten and the sweep is skipped with it. The comment on that branch promises
the opposite, and `render()` already implements the degraded shape
(`ok:false` + `refold_error`, `daemon.go:266-284`) — it is simply unreachable
from the tick path.

Recovery is real, which isolates the defect to the render decision: truncating
the torn bytes back to the last complete record made the SAME daemon fold
clean on its next beat, revert the hold (`role=owner reason=ttl_expiry`) and
rewrite `health.json` `ok:true`, `stale_by=0s`.

## Rung 2 — a torn line present when the daemon STARTS: `serve` panics and dies
##  → MISCHIEF-DF-40 (P1)

Same corruption, opposite order (tear first, then start `serve`):

```
mischiefd: run_dir=/tmp/df-mc-r18/live4/run boot=folding health=.../health.json
panic: runtime error: invalid memory address or nil pointer dereference
  reverter.(*Store).Close(0x0)      internal/reverter/journal.go:258
  reverter.(*Store).fold(0x0)       internal/reverter/journal.go:312
  reverter.(*Store).Refold(...)     internal/reverter/journal.go:356
  daemon.(*Daemon).revertExpired    internal/daemon/daemon.go:218
  daemon.(*Daemon).Run              internal/daemon/daemon.go:129
```

`reverter.Open` returns `(nil, s.foldErr)` on corruption
(`journal.go:246-248`); `daemon.New` tolerates the fold error (`isFoldError`)
but stores the **nil** store (`daemon.go:118`), and `Run`'s first act is
`d.store.Refold()` (`:129` → `:218`). So the comment saying the partially-valid
store lets the daemon "still report via health.json" is dead code, and the
operator gets a dead daemon and **no health document at all** — worse than the
stale-green document, because there is nothing to read.

This is also the defect behind the red CI: `go test ./...` at this HEAD fails
`internal/daemon` (17.9 s, rc=1) with the identical stack, panicking out of
`TestDaemon_SurvivesRefoldError` (`daemon_test.go:382`) — i.e. MISCHIEF-DF-39's
own regression test is the failing test INT-CI-7 was filed without. Local log
kept at `/tmp/df-mc-r18/gotest.log`.

## Rung 3 — asking the wounded daemon to STOP: SIGTERM hangs it forever
##  → MISCHIEF-DF-41 (P2)

With the torn journal in place and a hold live, `kill -TERM <serve pid>` (the
documented stop path; `cmd/mischief/main.go:418` handles SIGINT/SIGTERM) left
the process alive 10 s later — `state=S`, `wchan=futex_do_wait` — and only
SIGKILL ended it. The SIGQUIT goroutine dump names the trap:

```
goroutine 1 [sync.Mutex.Lock]:
  daemon.(*Daemon).render       internal/daemon/daemon.go:277
  daemon.(*Daemon).writeHealth  internal/daemon/daemon.go:244
  daemon.(*Daemon).Run          internal/daemon/daemon.go:145   <- ctx.Done() final beat
```

`writeHealth` takes `d.mu` and calls `render()` with it held; `render()`'s error
branch locks `d.mu` again (`:277-279`) — Go mutexes are not reentrant, so the
daemon blocks on itself. The comment directly above (`:274-276`) says not to
re-lock, and the code locks anyway.

## What an operator should take away

- The corruption ladder is currently split three ways: **survives but lies**
  (mid-run tear), **dies silently** (boot tear), **cannot be stopped**
  (tear + stop request). Each rung has a row with the exact command and the
  `file:line` of its cause.
- Until they land: after repairing a torn journal, the running daemon picks it
  up on its next beat (no restart needed); to stop a daemon whose journal is
  torn, use SIGKILL; and never trust `health.json` `ok:true` as proof that the
  daemon is folding — cross-check with `mischief status --dir <run>` (a separate
  process, which fails closed with `journal fold: ... unexpected EOF`, rc=1).

## Timing (Step 2b of the dogfood loop)

`hyperfine --warmup 3 --runs 20` on the same scratch dir, binary c28800dee641:

| operation | warm mean ± σ |
|---|---|
| `arm` (wrapper script with the sanction env) | 9.2 ms ± 0.6 ms |
| `status` | 5.4 ms ± 0.5 ms |
| `plan -f catalog/experiments/ex-001-noop-detector.yaml` | 4.2 ms ± 0.4 ms |
| `revert --all` (kill switch, 1 hold) | 0.01 s |
| TTL revert, measured inside the daemon | 112 µs – 178 µs (`duration_nanos` in the proof record) |
| health heartbeat | every 5.00 s (`HeartbeatInterval`) |

Nothing here is a wait a human would notice, so no PERF row was filed. The
staleness in Rung 1 is not slowness — it is a skipped write.

## Install leg (fresh machine, not skipped)

`scripts/bunker-plane.sh run --ttl=1h` on `bunker-mvp`: rc=0, agent `614e1669`,
8/8 evidence rows `pass` (preflight / build / spawn / ship ×2 / selftest /
collect / destroy), on-agent collect journal 38 lines `landed=13 reverted=13
skipped=9` rc=0, `TEARDOWN-GONE` with zero residue. Evidence file:
`/tmp/mischief-plane-evidence-20261017T154939Z-2727948.jsonl` (plane binary
stamped `c28800dee641`, i.e. the same HEAD this report describes).

## Board rows filed by this run

- `MISCHIEF-DF-40` (P1) — boot tear panics `serve` on a nil `*reverter.Store`;
  `health.json` never written; the red CI test is this defect.
- `MISCHIEF-DF-39-R2` (P1, recurrence) — mid-run tear: the tick loop skips
  `writeHealth`, so health stays `ok:true` and no TTL revert is attempted while
  the corruption persists. Cites DF-39 (fix 251371d).
- `MISCHIEF-DF-41` (P2) — SIGTERM/SIGINT self-deadlocks in `render()`
  (`daemon.go:277`); only SIGKILL stops the daemon.
- `MISCHIEF-DF-42` (P3) — the unknown-`--check-kind` refusal reports itself as
  `inverse kind` (`record.go:158`); the DF-29 whitelist itself was re-proven
  live and holds.
- Event on `INT-CI-7` naming the failing test (`TestDaemon_SurvivesRefoldError`)
  and its root cause.

## Artifact note — the legs above ran on a DIRTY checkout

The checkout at `/home/kara/mischief` carries an **uncommitted DF-39 patch**
(`internal/daemon/daemon.go` + `internal/reverter/journal.go`, mtime 09:10-09:14
today: the `isFoldError` tolerance in `New`, the `Store.foldErr` field, the
inline lock in `render`; `git diff --stat` also shows `daemon_test.go` and
`lifecycle_test.go` modified). `make bin` stamps `git rev-parse HEAD` (`c28800d`)
regardless of the tree, so the rungs above describe **that working-tree state**.

Every rung was therefore re-driven against a binary built from COMMITTED HEAD
alone (`git archive HEAD | tar -x -C /tmp/df-mc-r18/headtree && make bin`, stamp
`unknown` — no `.git` in the archive):

| rung | committed HEAD (c28800d) | dirty working tree |
|---|---|---|
| boot tear | `serve` exits rc=1 `serve: reverter: journal parse: unexpected EOF`, **no health.json**, no panic | **panic** (nil `*reverter.Store`) — MISCHIEF-DF-40's rung |
| mid-run tear | alive, `ok:true`, stale 14 s, no `refold_error`, target FAULTED past TTL (`daemon.go:138-140`) | same stale-green behavior (`daemon.go:148-160`) |
| tear + stop request | SIGTERM hangs; dump `setRefoldErr` (`:163-166`, called from `render` `:249`) → `writeHealth` → `Run` | same hang, lock written inline (`:277`) |

So the split is: **committed HEAD implements only the TICK half of DF-39's fix**
(the boot half its own regression test asserts was never implemented, which is
why CI is red at f473131), and the tick half carries both defects described
above; the in-flight attempt to add the boot half panics because
`reverter.Open` hands back a nil store. Rows `MISCHIEF-DF-40`,
`MISCHIEF-DF-39-R2` and `MISCHIEF-DF-41` carry this note in their `reasoning`.

