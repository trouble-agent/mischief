# Dogfood integration report — the `arm` verb's first real use (2026-10-16, run 15)

**Angle:** no prior dogfood run drove `mischief arm` — the CLI write-ahead surface
added by the DF-05 fix (8137f85, merged 2026-10-14). Every earlier serve/watchdog
run had to hand-write `reverter.jsonl` records. This run is the first consumer of
the documented arm→serve→TTL/kill-switch loop exactly as README.md §"Arming a hold"
teaches it. Binary: `bin/mischief` stamped `1b53e52a6abf` (= HEAD 1b53e52, built
2026-10-16 07:17). Environment: the fleet main host (control host) with a
reason-bearing marker at `$HOME`-equivalent scratch + `MISCHIEF_SANCTION_FILE`
(the README's L1 recipe), scratch run dir `/tmp/dogfood-mc-r15/runs`, all faults
landed by hand on scratch files the run created itself.

## What a user does (the loop that now works end-to-end from the CLI)

```sh
# 0. scratch target + its backup (the inverse restores from this)
mkdir -p /tmp/dogfood-mc-r15/{runs,target}
printf 'original-state-v1\n' > target/config.toml
cp target/config.toml target/config.toml.bak

# 1. ARM — write-ahead record (inverse + check fsynced BEFORE any fault may land)
MISCHIEF_SANCTION=1 MISCHIEF_SANCTION_FILE=$S/marker bin/mischief arm \
  --fault S-001 --target scratch:/tmp/dogfood-mc-r15/target --ttl 30s \
  --inverse-kind restore-file \
  --inverse-params path=…/config.toml,backup=…/config.toml.bak \
  --check-kind file-matches-backup \
  --check-params path=…/config.toml,backup=…/config.toml.bak \
  --dir /tmp/dogfood-mc-r15/runs
# -> armed a93ec97d… fault=S-001 target=… ttl=30s; exit 0

# 2. STATUS folds the journal — hold shows armed with ttl_remaining
bin/mischief status --dir …/runs

# 3. LAND the fault yourself (arm writes the hold only — landing is the fault
#    verbs' act, and this scratch leg landed it by hand: printf FAULTED > file)

# 4. WATCHDOG: `serve` reverts at TTL expiry from journal bytes alone
bin/mischief serve --dir …/runs     # -> health.json, revert_proof in journal

# 5. VERDICT: journal proof line
# {"type":"revert_proof","hold_id":"a93ec97d…","outcome":"reverted",
#  "fields":{"detail":"check: same","reason":"ttl_expiry","role":"owner",…}}
# target file byte-identical to the backup afterwards.
```

## The failure ladder (all driven; the refusals are the product)

| leg | input | observed |
|---|---|---|
| sanction gate, sanitized env | `env -u MISCHIEF_SANCTION -u MISCHIEF_SANCTION_FILE arm …` | `rails: host_not_sanctioned: host "karaHermes-mde-7840hs" is not sanctioned … nothing was written`, **exit 2, zero files in the run dir** — SPEC-13 held |
| usage validation | `arm --fault S-001 --dir …` (5 flags missing) | names every missing flag: `arm: --target, --inverse-kind, --check-kind, --inverse-params, --check-params, --ttl required (no default hold: arming nothing is not an arm)`, exit 2 |
| malformed params | `--inverse-params "path=X backup=Y"` (space, not comma) | `arm: reverter: arm: inverse restore-file backup is not readable: missing backup param`, exit 1 |
| unknown inverse kind | `--inverse-kind delete-everything` | **ACCEPTED, exit 0, hold journaled** — see finding DF-29 below; the poison surfaces later as `revert_failed` |
| unreadable backup (DF-23 fix) | `--inverse-params backup=$S/does-not-exist.bak` | refused: `backup is not readable: … no such file or directory`, exit 1 |
| poison at TTL | the unknown-kind hold under `serve` | boot reconcile executed it at expiry: `outcome=revert_failed detail=inverse failed: inverse kind "delete-everything": unknown (refusing to execute); check: present` — honesty held (no false green), but arm-time should have refused |
| kill switch mid-TTL | remove-file hold + hand-landed `leftover.tmp`, `revert --all` | `reverted 2d1a147e… detail=check: absent (0s)`; `kill switch: 1 holds reverted, wall=4ms (AC-6 budget 2s)`; file gone |

## The one finding: MISCHIEF-DF-29 (arm accepts inverse/check kinds revert refuses)

arm.go's own doc comment says *"Unknown kinds are refused here at arm time (the
same fail-closed grade a revert would give them, surfaced a whole phase earlier)"*
— the second half is false. `Lifecycle.Arm` (internal/reverter/lifecycle.go:119)
runs `Hold.Validate()` (record.go:168), which checks structure only (non-empty
kind, no empty params); the kind whitelist lives in the EXEC path
(internal/reverter/exec.go:39 `inverse kind %q: unknown (refusing to execute)`).
So `arm --inverse-kind delete-everything` journals a write-ahead record whose undo
can only grade `revert_failed` — exactly the "book a fault whose undo cannot work"
shape the DF-23 fix (validateInverseUndo, lifecycle.go:116) exists to prevent at
arm time for restore-file. The closed vocabulary is two inverse kinds and two
check kinds (README §"Arming a hold" states it); validating it at Arm is a
whitelist add in `Hold.Validate`, nothing more. Not a safety hole (revert refuses,
verdict stays honest, kill switch still works) — a contract gap between the verb's
own doc comment and its behavior.

## Honest-incident note (probe hygiene, not a product defect)

The run's FIRST unsanctioned-refusal probe was contaminated: the terminal session
inherited `MISCHIEF_SANCTION=1` from a prior run's exported env, so the "no
sanction" arm succeeded and looked like a gate bypass. Re-run with `env -u` on
both vars produced the true refusal (exit 2, nothing written). The gate is fine;
the lesson is that sanction-state leaks through long-lived agent shells — probe
the refusal from a sanitized env, and treat an unexpected SUCCESS on a should-refuse
leg as env suspicion before filing.

## What serve's boot reconcile adds for a real operator

A hold armed, then the arming process died, then `serve` started AFTER the TTL had
already expired: reconcile executed the inverse immediately, graded from the journal
(role=owner, reason=ttl_expiry) and wrote the proof. The watchdog owns holds it
never saw armed — the "outlives the CLI" promise holds through the CLI door now.

## Verdict on this surface

SHIPPABLE. The README's documented arm→serve loop works verbatim for both inverse
kinds, both measured checks, the sanction gate, the kill switch, and boot
reconcile; time-to-first-success for a reader of the README section is under two
minutes. One P2 contract gap (DF-29) is the only defect found.
