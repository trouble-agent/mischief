# Dogfood 2026-10-18 (19th tick b) — the AC-17 real-provider gate ladder

**Lane:** mischief-chaos-dogfood (second instance of tick `mischief-chaos-dogfood-2026-10-18-06-48-21`;
the sibling instance landed MISCHIEF-DF-43/44/45/46 in commit `3fef8ad` for the full-matrix + filing
surface — cross-cited, not re-swept).

**Artifact:** clean-HEAD export of `2d1ed37` (`git archive HEAD` -> `/tmp/df-mc-r19/src`), built with
go1.26.9. The working tree's uncommitted DF-39 patch (`internal/daemon/*.go`, `internal/reverter/*.go`,
dirty since 2026-10-17 09:10) is deliberately excluded from the export, so every number below is
committed-HEAD provenance.

**Promise under test** (README:39): the fault catalog's simulated cloud resource is "opt-in, allowlisted
and spend-capped", and the PRD's AC-17 says a real-provider fault requested without the triad "refuses
and runs against the simulator instead — proof: command output naming the refusal, plus the simulator's
own request log showing the fault landed there."

## What was driven (first time by any tick)

`grep -iE 'allow-real|spend-cap' .coding-hermes/dogfood-log.md` = 0 hits across 18 prior runs: the 9
layer-I `sim` shapes had been exercised, the gate itself never was. Ten legs, exit status read directly
(never through a pipe), scratch dir `/tmp/df-mc-r19`, sanctioned env
(`MISCHIEF_SANCTION=1` + reason-bearing `MISCHIEF_SANCTION_FILE`).

| leg | command (shape I-003) | rc | output |
|---|---|---|---|
| 1 | (nothing) | 0 | `backend_sim: I-003: simulator: ... requires the explicit --allow-real opt-in and an allowlisted resource tag and a spend cap above zero` + `ac17-redirect: ... landed in the simulator: /fault -> 429 ... — request log ... proves it` |
| 2 | `--resource aws:sandbox-lab` | 0 | refusal names opt-in + cap (resource leg correctly not named) |
| 3 | `--allow-real --resource aws:sandbox-lab` | 0 | refusal names the cap |
| 4 | `--allow-real --spend-cap 0.50` | 0 | refusal names the resource tag |
| 5 | `--allow-real --resource aws:sandbox-lab --spend-cap 0.50` | 0 | `backend_sim: I-003: real` + `ac17: real adapter is a v0.1 documented stub — landing in the simulator` |
| 6 | same, `--spend-cap 0` | 0 | refusal names the cap (above zero) |
| 7 | same, `--spend-cap -1` | 0 | refusal names the cap |
| 8 | `--allow-real --resource aws:prod-db --spend-cap 5` | 0 | **`real`** — a production-named tag admits the real plane |
| 9 | `--allow-real` only | 0 | refusal names tag + cap |
| 10 | `--resource ... --spend-cap 0.50` (no opt-in) | 0 | refusal names the opt-in |

The per-leg refusal wording is accurate — each leg names exactly the pieces that are missing. The
redirect proof is real: every refusal leg writes a request-log line
`simlog: <ts> GET /fault -> 429 faulted shape=api-rate-limit 429 burst with Retry-After=2`.

## Findings (board rows)

**MISCHIEF-DF-47 (P2) — the allowlist leg is vacuous.** `--resource '   '`, `'*'`, `aws:prod-db`,
`prod-fleet-main-host` and any other non-empty string print `backend_sim: I-003: real`; only `''` is
refused. Both gates test emptiness: `internal/backend_sim/refusal.go:67` (`g.ResourceTag == ""`, the
gate `sim` builds) and its twin `internal/rails/scope.go:123` (`s.Allowlisted == ""`). No allowlist data
exists anywhere (`find . -iname '*allowlist*'` = 0 hits). Four documents promise otherwise: README:35,
`docs/prd/mischief-v0.1.md:249`, `docs/SPEC-PLAN.md:115`, and the flag help `cmd/mischief/sim.go:43`.
Latent while v0.2's adapter is a stub; it is the rail that will guard the first real call.

**MISCHIEF-DF-48 (P3) — the `destroy_class=permanent` refusal has no operator surface.** `cmdSim`
hardcodes `gate.Decide("reversible")` (`cmd/mischief/sim.go:68`); no flag, no catalog field, no other
caller (`destroy_class` exists only in the Decide branch and tests). `sim --destroy-class permanent`
-> rc=2 `flag provided but not defined`. The test guarding the documented refusal calls
`gate.Decide("permanent")` directly and says in its own comment that the verb cannot express it
(`cmd/mischief/sim_test.go:134-146`).

**MISCHIEF-DF-28-R2 (P3, recur of DF-28) — the load gate is still missing from the cloud-plane landing
path.** One host, one minute, loadavg 1m 27.81: `battery --quick` rc=2
(`load_gate: host saturated, fault refused — measured loadavg 1m 27.81 >= gate 12.00`), `selftest --all`
rc=0 with 22/22 SKIPped naming `load_gate`, while `sim` armed and landed (rc=0 + request-log proof).
`cmd/mischief/sim.go` imports no rails package at all; the gate is constructed only in
`internal/battery/runner.go:325` and `internal/selftest/entry.go:26`. The top-level help still claims
"load gate on every landing path" (`cmd/mischief/main.go:165`).

**MISCHIEF-DF-49 (P2) — the DF-39 regression test tears the wrong file.** On the clean export,
`go test ./internal/daemon/ -count=1` -> rc=1, `--- FAIL: TestDaemon_SurvivesRefoldError (2.01s)` /
`daemon_test.go:397: expected ok:false on torn journal, got ok:true`, deterministic 3/3 repeats.
The fixture writes `<dir>/journal.jsonl` (`daemon_test.go:371-375`) while the store's journal is
`<dir>/reverter.jsonl` (`internal/reverter/journal.go:19`), so the torn record is never read, the
daemon boots clean, and the test can never pass. This is the red behind INT-CI-7 (still failing at HEAD:
CI run 39047592819 on `2d1ed37`), and it also corrects the provenance of MISCHIEF-DF-40's CI evidence:
the nil-Store panic is dirty-worktree-conditioned (that patch renames exactly this fixture), while clean
HEAD fails the assertion. Events 139 (INT-CI-7) and 140 (DF-40) carry the raw evidence.

## Install leg (proven, not skipped)

`scripts/bunker-plane.sh run --ttl=1h --evidence=/tmp/df-mc-r19/plane-evidence.jsonl` on `bunker-mvp`:
PLANE pass, agent `174045e7`, 8/8 evidence rows pass — preflight, build (`2d1ed37b5cdf`, 12111585 B),
spawn (1h TTL, exec gate passed; attempt 1 hit the documented orphan-safe retry after failing the exec
gate and was destroyed), ship (frozen HEAD 766176 B tar, 275 tracked files, go.mod verified) + binary
sha256-verified, collect (journal 38 lines sha256-verified, landed=13 reverted=13 skipped=9),
selftest (remote `selftest --all` 13 pass / 9 skip / 0 fail, rc=0), destroy (TEARDOWN-GONE). Independent
`bunker list` recheck ~2 min later: neither `174045e7` nor `7ad0b1ab` listed (zero residue).

## Performance (Step 2b)

`hyperfine --warmup 3 --runs 20`: `sim --shape I-003` **21.2 ms ± 4.7 ms** (user 2.4 ms, sys 8.3 ms),
`plan -f ex-001-noop-detector.yaml` **10.2 ms ± 2.4 ms**; `battery --quick` refusal 0.00 s;
`selftest --all` (all skipped by the gate) 0.03 s; every ladder leg 0.01–0.04 s cold/first-run. The
system-time dominance says the cost is process start plus the local HTTP server, not product code. No
user-noticeable wait anywhere, so **no PERF rows** — a win nobody can feel is not a finding.

## Verdict

✅ **SHIPPABLE on this surface, with three honesty gaps.** The gate decides correctly and the
redirect-to-simulator proof is real and durable (request log, not memory); the refusal wording names
exactly the missing pieces. What the gate cannot do is what the docs say it does: refuse a resource
outside an allowlist (DF-47), express the permanent-destroy refusal it advertises (DF-48), or gate the
one cloud-plane landing path while the help claims every path is gated (DF-28-R2).
