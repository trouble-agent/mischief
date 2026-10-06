# BATTERY-PARITY — `battery --quick` replaces the five bunker-qa.sh chaos cells (MSF-016, M4)

Status: parity PROVEN — the parity matrix executes live on the L0 scratch
harness with landed-proofs. Two committed runs are the evidence:
`docs/battery/quick-f34dfe1923585614/` (MSF-011, docker-absent host:
4 pass + the named `I-002` skip) and
`docs/battery/quick-c26bd0e3c51a85d1/` (MSF-016, docker-present host:
5 pass, 0 skip, 0 fail — every cell's real actuator executed). The five
legacy bash cells in
`~/…/bunker-qa.sh` are now SUPERSEDED: their replacements are
named, engine-graded, and covered by the S-1/S-5 tests
(`internal/battery/parity_test.go`, `internal/battery/matrix_test.go`).
Deletion of the bash cells is a FLEET-SIDE edit (the file is outside this
repo) — the checklist below is what that follow-up executes.

## Why the cells had to go (QA-TROUBLE-3 shape)

QA-TROUBLE-3 was the defining failure: a `cmd/`-layout repo whose "start"
command never started the app, yet `chaos-corruption` graded **OK** — the
verdict rested on the start command's EXIT CODE, not on any measured
landing. That shape produced verdicts about the HARNESS, not the product
(QA-TROUBLE-2/3/6/8, QA-19/20). The replacement contract is S-1:

- start command not real (the landing path never proves) ⇒ **`no_op`**,
  the run FAILS, and the journal carries **no `fault_landed` record**;
- start command real (measured landed proof + measured revert) ⇒
  **`recovered` / `not_recovered` / `corrupted`** — engine-graded on
  collected evidence, never on an exit code (AC-13).

Both arms are pinned by tests against the real quick matrix
(`TestS1ReplayNoLandedProofGradesNoOp`, `TestS1ReplayRealStartGradesRecovered`,
`TestS1ReplayRealStartBrokenRecoveryGradesNotRecovered`).

## Parity table (S-5) — old cell intent → new battery case

The same failure each bash cell attempted is now graded by a catalog
primitive whose landing is PROVEN out-of-band before anything is graded
(the SPEC-05 engine's rule 5: armed-but-unproven ⇒ no_op).

| # | legacy cell (bunker-qa.sh) | old intent | replaces with | target / failure graded by the new primitive |
|---|---|---|---|---|
| 6 | `chaos-disconnect` | point `HTTP(S)_PROXY` at a dead port (127.0.0.1:9); fail fast or hang | `N-012` http-error-injection | dead-proxy = the rate=1.0 end of N-012's userspace-proxy fault axis, on a scratch HTTP client; landed-proof = proxy hit-ledger delta (the cell never had one); verdict from probe samples, not rc |
| 7 | `chaos-shutdown` | `compose stop` (SIGTERM) → `start` → `kill` (SIGKILL) → `start` must recover | `I-002` dependency-cold-return (mode stop/kill/fresh-empty) | dependency dies and must return on the same address; landed-proof = external probe + state diff; tier L3 (docker API) — on an unprivileged L0 harness the cell records the named AC-11 skip, never a fake pass |
| 8 | `chaos-corruption` | truncate a state file, restart, observe, restore | `F-009` file-replaced-under-live-writer (replace mode) | the 08-29/30 archive-hole class; landed-proof = inode identity (`stat(path).inode != fstat(fd).inode`); pre/post byte-compare makes a vacuous pass impossible |
| 9 | `chaos-resource` | run the suite under a 3G `ulimit -v` cap; survive or OOM | `R-001` memory-max-squeeze | a real cgroup memory.max on a scope with cgroup-counter proof (a child re-exec cannot dodge it, unlike a shell ulimit); the cap-applied evidence is REQUIRED in the verdict (the old cell's vacuous-cap trap class) |
| 10 | `chaos-errorpath` | run the binary with its config missing; clean usage or panic | `F-009` file-replaced-under-live-writer (unlink-then-create mode) | the file a boot path needs is GONE from under the process; shares the primitive with chaos-corruption because one fault class genuinely covers both shapes — recorded as the shared mapping, not a fifth primitive |

Run id `f34dfe1923585614` (the committed live run) graded: `chaos-disconnect`
→ recovered, `chaos-shutdown` → aborted (named skip: docker API absent on
the unprivileged scratch host — the replacement exists; its live actuator
arrives with the container backend), `chaos-corruption` → recovered,
`chaos-resource` → recovered, `chaos-errorpath` → recovered. 4 pass, 1
named skip, 0 fail.

## Replacement command line

    mischief battery --quick --file-rows --dir <run-dir> --out <artefact-dir>

- `--quick` is the fixed parity matrix (exactly the five cells above, in
  bunker-qa.sh cell order); it ignores `--project`/`--primitive`.
- `--file-rows` emits `findings.jsonl` (AC-18 board-vocabulary rows for
  every adverse verdict; each row carries the run id in `run_id` — the
  AC-9 correlation key shared with the journal records and the CLI's
  `verdict:` line — plus the journal path in `reasoning`).
- Artefacts: `matrix.json` / `matrix.md` (fault × detection × recovery,
  S-5), `battery.jsonl` (SPEC-02 journal).
- Exit code 0 on green-or-skip; every adverse verdict files a finding.

## Deletion checklist (fleet-side follow-up — NOT this repo)

The five cells live in `~/…/bunker-qa.sh`, a harness-owned script
OUTSIDE this repo. The foreman follow-up executes exactly this:

1. Delete the five cell blocks from `bunker-qa.sh` (the `run()` battery,
   `cell` lines included):
   - `# 6 · chaos-disconnect — network cut via dead proxy` block (through
     its `disc_window`/`disc_rc` grading `fi`)
   - `# 7 · chaos-shutdown — SIGTERM stop/restart + SIGKILL recovery (docker path)` block (through its `else cell chaos-shutdown N/A "no compose file"; fi`)
   - `# 8 · chaos-corruption — truncate a state file, OBSERVE the next start, restore` block (through the project-probe hook's closing `fi`)
   - `# 9 · chaos-resource — suite under 3G memory cap` block (through its final `else cell chaos-resource FAIL ...; fi`)
   - `# 10 · chaos-errorpath — missing config: clean usage or panic?` block (through its `cell chaos-errorpath INFO "no start command detected..."` `fi`)
2. Delete the chaos-only helpers whose ONLY callers are the deleted
   blocks (verify with `grep -c <name> bunker-qa.sh` before and after —
   if the count drops to the definition itself, nothing else calls it):
   - `corr_grade()` and `state_ref()` (only caller: chaos-corruption's
     generic truncate path)
   - `ep_probe()` + `ep_grade()` (only caller: chaos-errorpath)
   - `capped_fail_summary()`, `go_panic_timeout()` +
     `go_panic_timeout_cause()`, `go_toolchain_oom()` +
     `go_toolchain_oom_cause()` (only callers: chaos-resource's grading
     chain)
   - `cap_probe` + the `CAP_KB`/`PARENT_CAP`/`SUBSHELL_CAP`/`cap_seen`/
     `sub_seen`/`par_seen`/`cap_note` plumbing (only consumer:
     chaos-resource)
   - KEEP: `build_incomplete`(+`_cause`), `go_suite_env_failure`,
     `suite_deps_missing`(+`_cause`), `native_runner_missing`(+`_cause`),
     `vacuous_native_suite` — the ci-pass leg (lines ~1694-1728) still
     consumes all of them, so deleting them breaks the surviving battery.
3. Re-point the battery header comment (top of file: "chaos-disconnect ·
   chaos-shutdown · chaos-corruption · chaos-resource · chaos-errorpath")
   to the replacement: `mischief battery --quick` (this repo,
   `github.com/trouble-agent/mischief`).
4. Update the DAG/monitor cells that parse the five `chaos-*` cell ids from
   the evidence file — the ids move to `q1..q5` with
   `[replaces chaos-*]` annotations, or to the findings rows.
5. Prove the edit: run `bunker-qa.sh run` against any small repo and
   confirm the evidence carries no `chaos-*` cells, then run
   `mischief battery --quick --file-rows` on the same repo and attach the
   matrix artefact next to the evidence.

The replacement command the checklist installs: `mischief battery --quick
--file-rows` (binary built with `make bin` from this repo; requires the
SPEC-13 sanction marker on the executing host).
