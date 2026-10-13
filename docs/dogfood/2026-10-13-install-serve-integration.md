# mischief dogfood — install/serve reverter report (2026-10-13, run 6)

Run: mischief-dogfood lane, tick 2026-10-13-10-02-10. Angle: the ONLY surfaces
no prior tick drove as a fresh user — the **ex-001 experiment plan path** and a
**full fresh-machine install + serve re-drive on a new ephemeral bunker agent**
(5 prior ticks tested on the control host; this one re-proved the install from
scratch on bunker-mvp). Code HEAD 33a0336.

## Fresh-machine install leg (bunker-mvp agent df-mischief-sv13, TTL 45m)

| step | result | time |
|---|---|---|
| `bunker spawn --server bunker-mvp --ttl 45m df-mischief-sv13` | rc=0, bundle + key | 27s |
| ssh reachability (go 1.22.2, git, make present) | ok | <5s |
| `git clone https://github.com/trouble-agent/mischief.git` (public README origin) | rc=0 | 3s |
| `go build -o bin/mischief ./cmd/mischief` (auto-downloaded go1.26.9 toolchain + yaml.v3) | rc=0 | 194s |
| `./bin/mischief version` | `mischief unknown` — unstamped build (expected: plain `go build`, README Makefile `make bin` stamps; a plain-clone user gets "unknown" honestly) | 0s |
| unsanctioned selftest refusal | `rails: host_not_sanctioned … nothing was written` rc=2, fail-closed SPEC-13 | 0s |
| `MISCHIEF_SANCTION=1` S-001 selftest | `1 pass, 0 skip, 0 fail` ADMITTED at L1 | 0s |
| `MISCHIEF_SANCTION=1` selftest --all | **13 pass, 8 skip, 0 fail (of 21)** — skips are honest named capability gaps (no docker, no NET_ADMIN, no shim) | <1s |
| `MISCHIEF_SANCTION=1 serve --dir ~/serve-run` | health.json written atomically, ok:true | ~30ms |
| `bunker destroy` (after killing leftover serve) | destroyed, no residue at 90s recheck | — |

**Install verdict: HOLDS.** A fresh Debian user with only git+go clones, builds
and reaches green selftests in ~4 minutes (194s of it the one-time toolchain
download). Not a silent pass: agent destroyed, `bunker list` recheck clean.

## The plan path — a new user's first documented experiment FAILS

`mischief plan -f catalog/experiments/ex-001-noop-detector.yaml` (the ONE
experiment EXPERIMENTS.md marks "runnable today"): exit 2, notes
`F-008: not in the loaded catalog` + refusal `scope_below_minimum`, on BOTH the
fresh bunker clone and the control host. F-008 has no descriptor in
catalog/faults (10 files, F-009 present). → **MISCHIEF-DF-22 (P2)**.

## The reverter lifecycle, driven for real end-to-end (control host)

Using the documented journal interface (DF-05's hand-authoring path — this run
went through `Lifecycle.Arm` in-process, the first non-test consumer ever):

- arm (write-ahead, content-id 52d4906b…) → land → status shows
  `armed … ttl_remaining` counting down live → **TTL expiry revert measured
  `check: same`** with the file byte-restored (run3 leg, backup present).
- The failure leg (run1, backup absent): the daemon held the fault the full
  30s TTL, then graded `revert_failed` honestly — health.json flips
  `ok:false, revert_failed:1`, journal carries the exact missing-file detail.
  The honesty ladder held. **But the arm side never checked that the backup
  path was readable** — the fault sat live for the whole TTL on a hold whose
  undo could never execute. → **MISCHIEF-DF-23 (P2)**.
- kill switch `revert --all` on an empty/terminal dir: honest `0 holds
  reverted, wall=0s (AC-6 budget 2s)` rc=0.
- `doctor --self-check` on the control host: correctly **rc=1** naming the
  FAILING rail (load_gate, loadavg 15.06 ≥ gate 12) while sanction/scope pass
  — the exit-code contract is honest under load.
- selftest --all on control host: 17 pass / 4 skip / 0 fail, rc=0.

## Perf (Step 2b)

Nothing a user would notice: plan/selftest/status/doctor all sub-10ms warm;
the only real waits are the 194s one-time fresh build (toolchain download) and
the ~30s spawn — both inherent, neither a product defect. No PERF rows,
deliberately.

## New user friction inventory

1. ex-001 plan dead end (DF-22 above) — the docs' own advertised runnable
   experiment refuses.
2. The docs never name how to reach the reverter (DF-05, still pending) — a
   serve/TTL user must read internal/reverter source or the dogfood reports.
3. `bunker destroy` orphan-gate remediation dead end (DF-24, P3).
