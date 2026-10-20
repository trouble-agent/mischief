# Dogfood report — TTL containment & the protected-target rails (2026-10-20, tick 23)

- **Lane:** `mischief-chaos-dogfood-2026-10-20-13-25-36` (dogfood lane for `mischief-chaos`)
- **Target:** `/home/kara/mischief` @ `337ebe58085db8789e71adc7b141bca62992b2e5`
- **Build:** clean clone (`git clone --no-hardlinks file:///home/kara/mischief /tmp/df-mc-r23b`), `make bin` rc=0 in ~45s, `bin/mischief` 12,165,511 B, `-X main.gitSha=337ebe58085d`
- **Host:** dev host `karaHermes-mde-7840hs` (unsanctioned by SPEC-13; `MISCHIEF_SANCTION=1` used only where the product requires it, and **no fault was ever landed outside a `/tmp` scratch file this run created itself**)
- **Mode:** read/observe + file rows. No product source, board row, docs/dogfood history, scheduler state, permission or credential was modified. The only repo writes are this report, the `dogfood-log.md` entry and the `skills/mischief-usage` corrections (the dogfood contract's "leave the knowledge behind" half).

## Promise under test

> "Hold a fault under a TTL owned by a reverting watchdog that outlives the CLI; protected targets refuse at resolution; the watchdog reverts the hold at TTL expiry."

Everything below was driven as a user of the built binary, not by reading the source.

## 1. What the watchdog actually promises (the headline) — WORKS, with a coarse clock

The whole arm → land → watchdog-reverts path was driven end to end with the fault landed **by hand** on a scratch file (the `run` verb that would land it is not implemented in v0.1):

```
serve --dir RUN &                                    # daemon up, health.json ok:true
arm --fault S-001 --target scratch:SC --ttl 10s --inverse-kind restore-file ... --dir RUN
printf 'CORRUPTED' > SC/config.toml                   # the "landed" fault
# poll the file every 50 ms until it reads back 'original'
```

| declared TTL | revert observed at | lag |
|---|---|---|
| 10s | 9.27s | **-0.73s** |
| 10s | 14.21s | +4.21s |
| 10s | 14.22s | +4.22s |
| 10s | 14.21s | +4.21s |
| 10s | 14.21s | +4.21s |
| 5s | 4.30s | **-0.70s** |
| 5s | 9.28s | +4.28s |
| 5s | 4.37s | **-0.63s** |

Every revert was verified by **the target file's own bytes** (`original` restored) and cross-checked in `health.json` (`reverted:1, revert_failed:0`) and `status --all` (`reason=ttl_expiry`) — not by trusting the product's report.

Reading: the fold is a periodic ~5s tick (reverts cluster at declared-TTL +4.2s, i.e. the first tick after expiry for a daemon started ~0.8s earlier), and the expiry clock appears to truncate the arm timestamp to whole seconds (the lone early run). So the containment point is **not** the declared TTL: the fault can stay landed ~one tick too long, and occasionally the inverse fires just before the TTL the operator asked for. Filed as **MISCHIEF-DF-59** (P4) — with the fix direction being either a per-hold timer or publishing the tick.

Also re-verified working: **boot reconcile** — a fresh `serve` on a run dir carrying an armed, long-expired hold (and a corrupted target) reverted it within one tick (`role=owner reason=ttl_expiry`, file restored). And `serve` stopped cleanly with a plain `kill` on this HEAD.

DF-30's boot-time-snapshot defect is FIXED (holds armed **after** the daemon started are reverted — see the table; the whole test armed after `serve` was up). DF-32 is FIXED: three consecutive `serve` processes produced three distinct `daemon_boot` ids (`5d387c00bc1afead`, `55e5526bb5b1b018`, `8038b874f5edd71f`). Both fixes make the existing `skills/mischief-usage` notes stale; corrected there.

## 2. The protected-target rails — refuse in `plan`, absent in `arm`

```
$ plan -f catalog/experiments/ex-004-live-file-replace.yaml
plan: protected target "gateway" refused: fleet control plane: a fault pointed at it by
accident is an outage, not an experiment — an operator may pass the explicit override flag,
and the run then records it in the journal (exit 2)                          rc=2
```

Two findings from that one surface:

- **MISCHIEF-DF-57 (P3)** — the override flag the refusal names **does not exist**. Census on this HEAD: `plan` ships `-catalog-dir -f -journal-dir -json -plan-only-sanction-skip -scratch-dir`; `arm` ships `-catalog-dir -check-kind -check-params -dir -fault -inverse-kind -inverse-params -target -ttl`. `grep -rn -iE 'override' README.md docs/*.md docs/prd/*.md` finds only the SPEC-13 *variable* override (`MISCHIEF_SANCTION_FILE`) plus the AC-7 prose contract; the PRD's planned `run` verb lists no override flag either. So AC-7's "runs only with the explicit operator flag and the journal records it" has no surface, and the refusal reads as operator error rather than "not in v0.1".
- **MISCHIEF-DF-58 (P3)** — the rail is **not evaluated on the `arm` path at all**: `arm --target process:scheduler` answered `armed 963a3763ced6d8f7 … rc=0` (no refusal, no advisory), the hold is written to `reverter.jsonl` (`payload.target=process:scheduler`), and `status --all` publishes it as a live hold — in the same session where `plan` refused the same protected selector. The write-ahead verb is exactly where an operator concludes "the product checked this target".

Non-findings on the same surface: `plan` with no target refuses rc=2 (`--target required`), `arm` with an unknown fault id refuses rc=2 before any write, the unimplemented `--plan-only-sanction-skip` stub does **not** weaken the sanction gate (still rc=2 unsanctioned), and `plan` on an unsanctioned host refuses fail-closed naming host + marker path + env override.

## 3. Absent ≠ clean (MISCHIEF-DF-60, P4)

```
$ revert --all --dir /tmp/df-mc-r23/does-not-exist-xyz
kill switch: 0 holds reverted, wall=0s (AC-6 budget 2s)     rc=0
$ status --dir /tmp/df-mc-r23/does-not-exist-xyz
run_dir: …does-not-exist-xyz / boot_id: 82d3352cbcc022e4 / holds: 0 / proofs: 0   rc=0
```

A typo'd (or rotated-away) run dir is indistinguishable from a verified-clean sweep on the product's one containment verb, and `status` even mints a `boot_id` for a directory that is not there. Contrast the neighbouring honesty: `audit` grades an uninstalled host as its own named condition.

## 4. Step 2b — what using it costs (PERF)

`hyperfine --warmup 3 -r 20` (warm, this host), plus one-shot cold runs:

| operation | warm mean ± σ | notes |
|---|---|---|
| `plan -f ex-001` | 11.3 ms ± 9.3 ms | cold first call: 0.00s real (`/usr/bin/time -p`) |
| `status --dir RUN --all` | 10.9 ms ± 5.0 ms | |
| `revert --all` (empty dir) | 8.3 ms ± 3.9 ms | |
| `doctor` | 4.6 ms ± 1.8 ms | |
| `audit --prefix P` | 3.7 ms ± 2.0 ms | |

Every verb is process-startup bound; nothing here is slow enough for a user to notice, so **no PERF row was filed**. The only user-visible wait in this run is the watchdog's TTL tick (section 1) — and that is a containment-contract finding with numbers, not a perf row.

## 5. Install leg — PROVEN, not skipped

`scripts/bunker-plane.sh run --ttl=1h --server=bunker-mvp` (evidence `/tmp/df-mc-r23-plane.jsonl`):

- preflight: `bunker-mvp ONLINE`
- build: frozen HEAD binary `337ebe58085d`, 12,165,623 B
- spawn: agent `57955f5a` (exec reachability gate passed)
- ship ×2 (repo archive + binary) pass
- **remote `selftest --all` rc=0** — collect: `journal=38 lines landed=13 reverted=13 skipped=9`
- destroy: `TEARDOWN-GONE: agent 57955f5a destroyed and absent from the bunker-mvp list (zero residue)`
- fold: **PLANE pass**, driver rc=0. 8/8 evidence rows `status=pass`.

## 6. Independent re-check of a pending P1 (not a re-file)

`QA-MISCHIEF-005` (P1, filed 12:08Z) reports `TestOwnerSurvivesKill9AndRevertsAtTTL` failing natively at HEAD `983b8d9`. On HEAD `337ebe5` (that commit is an ancestor) the package is **green**:

```
$ go test ./internal/reverter/... -count=1                     -> ok  internal/reverter  11.938s   rc=0
$ for i in 1 2 3; do go test ./internal/reverter/... -count=1; done
  ok 13.699s | ok 12.716s | ok 12.532s                                                (rc=0 each)
$ go test ./internal/reverter/ -run TestOwnerSurvivesKill9AndRevertsAtTTL -count=5
  ok  internal/reverter  29.455s                                                      rc=0
```

Four consecutive green runs (3 full-package + 1 five-fold run of the named test) on a host at loadavg 10–13.

No new row filed (the row exists and is pending; adding a duplicate would be board noise). Recommendation for its owner: re-run at HEAD before spending a worker — if it is green natively and red only under CI/3GiB, the row's remaining evidence is the CI-side cap, not a native repro.

## Findings filed (all append-only, `boardctl validate` → RESULT: OK)

| row | P | one line |
|---|---|---|
| MISCHIEF-DF-57 | P3 | protected-target refusal names an override flag that exists in no verb and no doc |
| MISCHIEF-DF-58 | P3 | the protected rail is not evaluated on the `arm` path — `arm --target process:scheduler` reports armed |
| MISCHIEF-DF-59 | P4 | the watchdog's TTL is tick-quantized + second-truncated: containment point ≠ declared TTL (measured -0.73s … +4.22s) |
| MISCHIEF-DF-60 | P4 | an absent run dir reads as a verified-clean kill switch |

Prompt hygiene note for the next tick: the reverter's TTL fold fires on a **~5s tick**, so any TTL observation window must exceed `ttl + 5s + ε`; two of this run's first three probes produced false "not reverted / regression" readings purely from a too-short window before the tick model was measured.

## Evidence map

| artifact | path |
|---|---|
| run dirs (scratch) | `/tmp/df-mc-r23/runs/{default,r24,r25,lat10-*,lat*,serve32b}` |
| leg logs | `/tmp/df-mc-r23/legs/*.log` (plan refusals, arming, sim, install/uninstall/audit, TTL series) |
| TTL measurement | `/tmp/df-mc-r23/legs/ttl-latency10.log`, `/tmp/df-mc-r23/legs/ttl-latency.log` |
| boot reconcile | `/tmp/df-mc-r23/legs/boot-reconcile.log` |
| reverter re-check | `/tmp/df-mc-r23/reverter-test.log`, `/tmp/df-mc-r23/reverter-repeat.log` |
| install leg | `/tmp/df-mc-r23-plane.jsonl`, `/tmp/df-mc-r23-plane.out` |
