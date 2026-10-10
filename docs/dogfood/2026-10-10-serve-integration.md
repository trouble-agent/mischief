# mischief dogfood — serve/watchdog integration report (2026-10-10, run 3)

Run: mischief-dogfood lane. Angle: the surface no prior dogfood touched —
`serve` (the reverter daemon), TTL expiry, kill switch, boot reconcile, revert
honesty, journal corruption posture. Prior runs covered the CLI/install/plan
surface (2026-10-02, 2026-10-08, 2026-10-10 02:30 sibling).

## What this surface promises (PRD §5, §6.1; SPEC-04)

Arm → land → hold under TTL, with the inverse recorded BEFORE the fault lands;
a watchdog that reverts at TTL expiry **even if the CLI died**; a kill switch
(`revert --all`) that proves reverted within 2s; boot reconcile marking
previous-boot leftovers `stuck`; `health.json` as the observable state file.

## How a real user reaches it (the honest path — and the friction)

The friction IS finding DF-05: no shipped verb can arm a hold. `Lifecycle.Arm`
(internal/reverter/lifecycle.go) has exactly one non-test call path: nothing.
selftest lands primitives through its own harnesses; sim arms the in-process
cloud simulator; `revert`/`status`/`serve` only CONSUME holds. So this run
drove the surface the way the SPEC-04 doc comment says the machinery intends —
the journal is the durable interface ("a detached owner re-reads every fact
from journal bytes") — by writing one arm record by hand:

    hold_id = sha256("hold\n<fault>\n<target>\n<ttl_ns>\n<inv_text>\n<chk_text>\n")[:16]
    inv/chk text = "<kind>\x1f<sorted k=v joined by \x1f>"   (id.go canonicalJoin)
    line = {"type":"arm","hold_id":H,"ts":<unix>,"boot_id":<REVERTER boot id from
            run/boot.json>,"payload":{fault_id,target,inverse,check,ttl,boot_id,arm_pid}}

Inverse/check kinds are the four declarative ones in exec.go: `restore-file`,
`remove-file`, `file-matches-backup`, `file-absent`. Native exec, no shell.

## What worked — measured (all numbers from the run transcript)

| operation | result |
|---|---|
| status, empty dir | 5 fields, exit 0, 17ms cold |
| status with armed hold | `armed … ttl_remaining=7s`, 5.2ms±1.9ms warm (hyperfine, 20 runs) |
| serve cold boot → health.json | 29–31ms (3 fresh dirs) |
| health.json @1s | ok:true, hold row armed, ttl_remaining_sec 6.97 |
| TTL expiry (8s hold) | daemon reverted: `role=owner reason=ttl_expiry detail=check: same`; faulted file byte-identical after (`original-state-v1`) |
| kill switch, 2 holds, under junk-writer load | 2 reverted, wall=7ms (budget 2s), rc=0 |
| revert_failed honesty | inverse w/ missing backup → `outcome=revert_failed detail=inverse failed: read backup … no such file`; exit 1; fault stays live and the journal SAYS so |
| corrupt journal line | status, serve, and `revert --all` all exit 1 loudly — no silent skip, no partial fold |
| foreign boot id on arm | graded `stuck` (safe direction: false stuck is surfaced+revertable, false current is invisible) |

The anti-false-green thesis (the project's whole reason to exist) held on
every leg: the revert verdict is measured (`check: same` against a backup),
the failure verdict names the exact missing file, and a corrupt journal is
never folded into success.

## The two contract traps a fresh integrator hits

1. **boot_id in the arm payload must be the REVERTER boot id** (run/boot.json's
   `reverter_boot`), not the host boot id from /proc/sys/kernel/random/boot_id.
   The host-id variant is instantly graded `stuck` with reason previous_boot —
   same live boot, false stuck. Direction is safe but undocumented (DF-06).
2. **Run dir defaults disagree with the docs**: CLI verbs default to
   `~/.mischief/runs/default`; internal/reverter/doc.go and DefaultDir say
   `~/.mischief/reverter` (DF-07).

## Right way, for the next agent

- Scratch dir everything; the journal format is stable and fold-validated —
  one bad line fails the whole dir loudly (by design; do not "fix" it by
  skipping).
- Read `boot.json` after a first `status` call; use THAT id in arm records.
- TTL is `payload.ttl` in nanoseconds; expiry = ts + ttl (fold: journal.go).
- kill switch budget is real: measured 7ms for 2 holds under load.
- The reverter's own logs go nowhere — `health.json` + `status` are the only
  observables; both were accurate at every point in the run.
