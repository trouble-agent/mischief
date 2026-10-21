# mischief dogfood — on-agent mechanism probe (2026-10-21, lane mischief-chaos-dogfood)

Tick `mischief-chaos-dogfood-2026-10-21-09-54-35`. Product HEAD
`95ee79682195852065b943b45b2e97e1d54c8605` (product code unchanged since the 25th tick's
`ac4bc1b`; the delta is board/QA/dogfood commits only).

## Promise tested

> A clean machine can install mischief and run its suite; when a fresh machine reds the
> reverter package, the project's own records say WHY — the run is honest about what is a
> product defect and what is the machine.

## Angle chosen (and why this, not another sweep)

Zero code delta since the previous tick, no unexercised surface left that a fresh run would
teach anything about, but ONE fresh claim existed about the current head: the QA lane's battery
at 09:20 (−05) red `TestOwnerSurvivesKill9AndRevertsAtTTL` natively, in `act`, and under the
3 GiB cap, and its own addendum (board event 212) could not identify the agent-side mechanism —
it reported that no 203/EXEC appeared and closed with "next battery must capture the failing
test's own dir + journal BEFORE the suite's cleanup".

So this tick drove the discriminating probe the QA lane asked for, on a FRESH ephemeral agent,
with the failing test's window captured before teardown.

## Method (exact)

1. Clean-machine battery via the standard harness (`~/.hermes/scripts/bunker-qa.sh launch
   /home/kara/mischief` → `collect`), agent `c978c347` @ `bunker-mvp`, head `95ee796`, evidence
   `/tmp/bunker-qa-evidence-20261021T101710Z-4057138.jsonl` (19 rows).
2. On the SAME agent, SAME tree, SAME session type, before any teardown, a mechanism leg ran the
   reverter package in four contexts and harvested the user journal + `cpu.max` + `GOMAXPROCS` +
   the run dirs' journal bytes. Raw harvest: `mech-leg.log` (144 lines) attached to the tick's
   scratch (`/home/kara/.hermes/work/mischief-df-20261021-0954/`).
3. Perf (Step 2b): `hyperfine --warmup 3 --runs 20 --ignore-failure` over the documented verbs.

## Battery cells (head 95ee796, fresh agent)

| cell | verdict |
|---|---|
| repo-head | OK tested HEAD=95ee79682195 |
| toolchain-bootstrap | OK go1.27.3 / gcc wrappers / make / compose plugin / buildx v0.38.1 / node v22.15.0 |
| fresh-install | OK |
| ci-pass | **FAIL** native rc=1 `TestOwnerSurvivesKill9AndRevertsAtTTL` (10.68s) internal/reverter; act leg rc=1 (Build OK, Vet OK, Main Test FAILED 53.1s) |
| upgrade | OK (v0.1.0 tree → HEAD reinstall clean) |
| docker-deploy | N/A no compose |
| ui-probe | INFO no long-running HTML surface |
| chaos-disconnect | UNVERIFIED (gated by ci-pass; probe rc=0 with empty output = the already-open harness row QA-BUNKER-56-R2) |
| chaos-shutdown | N/A no compose |
| chaos-corruption | N/A no db/state files |
| chaos-resource | **FAIL** rc=1 under cap=3145728KB, same test |
| chaos-errorpath | OK |

Install leg: **PROVEN, not skipped** — spawn → toolchain bootstrap → sync → documented build →
suite/act → upgrade from the v0.1.0 tree → destroy (TEARDOWN-GONE, 19 rows pulled).

## Mechanism leg (same agent, same tree)

| leg | command | result |
|---|---|---|
| A1–A5 | `go test ./internal/reverter/ -run TestOwnerSurvivesKill9AndRevertsAtTTL -count=1` | **rc=0 ×5** (5.19 / 5.50 / 5.53 / 5.67 / 5.54 s) |
| B | `go test ./internal/reverter/ -count=1 -v` | rc=1 (12.7 s) — **only** `TestRealSystemdRunLiveUnitEndToEnd` (0.92 s) |
| C | `GOMAXPROCS=1 go test ./internal/reverter/ -count=1` | rc=1 (13.1 s) — same test |
| D | `go test ./... -count=1 -timeout 1200s` (documented suite) | rc=1 (35.8 s) — same single red, 21 other packages ok |
| E | `go test ./... -count=1 -p 1 -timeout 1200s` | rc=1 (70.2 s) — same single red |

The product names the class itself, verbatim from C:

```
owner_spawn_test.go:270: real systemd-run detach failed (liveness gate rejected a real unit):
reverter: detached TTL owner could not be started: owner unit exited immediately
(systemd exec-formation class, e.g. 203/EXEC): unit=mischief-owner-3ad7185f04ee5048
exec_status=0 (show: load=not-found active=inactive sub=dead exec_status=0 pid=0)
```

…and the agent's user journal agrees (52 `mischief-owner` lines; four of the pair shaped exactly
like this):

```
Started mischief-owner-c8cdeebc741323d5.service - /tmp/go-build1816993971/b282/reverter.test
  __owner -dir /tmp/mischief-reverter-test-3501135340 -hold c8cdeebc741323d5 -expiry …
mischief-owner-c8cdeebc741323d5.service: Main process exited, code=exited, status=203/EXEC
```

The `ExecStart` target is the go test **transient build binary** under `/tmp/go-build<NNN>/`, while
`/bin/true` starts fine for the same user manager (QA event 212's control) — so the failing
ingredient is the exec TARGET path, not the manager's existence.

## What this does and does not settle

* **Settles (narrows) the class:** on this agent the exec-formation class is *reproducible*, and it
  reds exactly one test — the one that requires a **real** systemd-run owner and has **no**
  capability guard. Filed as **MISCHIEF-DF-66** (new row; no prior row named that test).
* **Refines QA event 212:** "no 203/EXEC" is wrong for `mischief-owner` units; the difference from
  its passing `/bin/true` control is the transient exec path.
* **Does NOT close QA-MISCHIEF-002-R2:** the AC-5 test passed 5/5 on the very agent whose journal
  shows those 203/EXEC units, and it passed inside both full-suite runs — so the battery's red of
  *that* test is not the exec-formation class; it survives via the setsid fallback. The residual
  AC-5 mechanism therefore narrows to a suite-context timing/contention flake (the battery ran it
  alongside `act` containers and the docker legs), consistent with every dev-box arm and hosted CI
  green at the exact head. Filed as an event on QA-MISCHIEF-002-R2; the row stays open.
* **Re-produces the row's premise:** the battery at a NEW head (95ee796) red the AC-5 test in
  native + act + cap, i.e. QA-MISCHIEF-005's three-environment claim still holds. Filed as an
  event on QA-MISCHIEF-005.

## Perf (Step 2b)

`hyperfine --warmup 3 --runs 20 --ignore-failure` (warm), and one-shot cold runs:

| verb | warm mean ± σ |
|---|---|
| `version` | 4.0 ± 1.6 ms |
| `status` (virgin HOME) | 8.9 ± 11.1 ms |
| `status --all` | 6.7 ± 4.2 ms |
| `doctor` | 9.4 ± 5.2 ms |
| `plan -f ex-001` | 4.4 ± 0.7 ms |
| `audit --json` | 4.3 ± 1.7 ms |
| `retention --dry-run` | 5.6 ± 2.9 ms |

Cold one-shot `plan` 0.00–0.01 s real. Everything is process-startup bound; nothing here makes a
user wait. **No PERF row, deliberately** — the only human-feelable wait on this product remains the
~5 s TTL fold tick already owned by MISCHIEF-DF-59.

Two probe notes with product relevance (no rows added):
* `hyperfine` aborts without `--ignore-failure` on `status` in a virgin HOME because the verb exits
  1 there — the same first-run friction already filed as MISCHIEF-DF-62.
* `plan` and `arm` need `catalog/` reachable from the cwd (or `--catalog-dir`): running the binary
  from another directory refuses with `catalog/faults not found from cwd upward`. Documented for
  `doctor` in the README; the refusal is honest and names the flag.

## Verdict

🟡 **PROMISING-BUT-ROUGH on the clean-machine story.** The product's own diagnostics are the
strongest part of this run: it detects the environment's systemd exec-formation gap, names the
unit id, the exec class and the manager's view, and refuses rather than pretending success. What
is rough is the *test* layer around that honesty: one live-systemd test has no capability guard,
so a fresh machine reds the package deterministically and the red reads as a product regression —
re-opening the same "agent-side mechanism UNVERIFIED" loop the QA rows have been circling.

## Artifacts

* Row **MISCHIEF-DF-66**; board events **213** (QA-MISCHIEF-002-R2 mechanism + discriminator) and
  **214** (QA-MISCHIEF-005 clean-machine re-production).
* This report; the dogfood-log entry; the DuckBrain run record.
* Scratch (not committed): `/home/kara/.hermes/work/mischief-df-20261021-0954/` (build, probe
  scripts, `mech-leg.log`, `perf/verbs.json`, ops logs).
* Read-only lane: no product code touched, no pre-existing row/event edited, no real target
  faulted (all scratch), no credential minted, repo visibility/permissions untouched. The
  ephemeral agents used were destroyed by the harness (`TEARDOWN-GONE`); one of this tick's own
  spawns (`df-mischief-mech`) never accepted its key and was destroyed by hand.
