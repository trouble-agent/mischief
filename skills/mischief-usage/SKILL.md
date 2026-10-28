---
name: mischief-usage
description: Use when working with or on the mischief repo (fault-injection tool for fleets of Linux daemons) — what exists, what runs, how to verify claims.
---

# mischief — usage for agents

mischief is a **shipped** fault-injection tool ("faults are data": landed-proof +
inverse + blast radius). **v0.1.0 is released**: tarballs (linux_amd64 / linux_arm64
+ checksums.txt) live at <https://github.com/trouble-agent/mischief/releases>.

**Entry point: the `mischief` CLI** — `cmd/mischief/`, built with `make bin` →
`bin/mischief` (CGO_ENABLED=0; `make bin` stamps the git sha, a release stamps the
tag), or the tarball's static binary plus prebuilt `libfault-anchored.so`.
Quick checks after install: `mischief version`, `mischief status`, `mischief doctor`.

## The CLI (v0.1.0 verbs — verified against `./bin/mischief --help` and every listed leaf's `--help`, HEAD 3352398e7963, 2026-10-24)

```text
plan      -f <exp.yaml> [--catalog-dir D] [--json] [--plan-only-sanction-skip] [--scratch-dir D] [--journal-dir D]  # zero side effects (AC-1);
                              # --journal-dir D appends one run_planned record to D/plan.jsonl
status    [--dir D] [--all] [--json]   # ACTIVE holds (armed/stuck) + TTL remaining, proofs;
                              # --all adds terminal holds (reverted/revert_failed)
revert    (--all | --hold <id> [--retry]) [--dir D]         # `revert --all` = the kill switch;
                              # --retry re-executes a revert_failed hold's recorded inverse
arm       --fault ID --target SEL --ttl D --inverse-kind K --inverse-params k=v,.. --check-kind K --check-params k=v,.. [--catalog-dir D] [--dir D]
serve     --dir D             # reverter daemon: TTLs + boot reconcile + health.json
doctor    [--catalog-dir D] [--json] [--ops] [--prefix P] [--self-check]         # exit 1 = a check FAILED
battery   [--catalog-dir D] [--dir D] [--quick] [--project P] [--primitive ID] [--out D] [--file-rows] [--dry-run]
chaos matrix  --project P [--catalog-dir D] [--markdown] [--tier-max N] [--out D]
chaos check   --lane F [--lane-dir D]
chaos finding --project P --board D --title T --reason R [--priority P] [--primitive ID] [--verdict V]
sim       --shape I-00N [--path P] [--log F] [--allow-real --resource TAG --spend-cap USD] [--destroy-class C]
install   [--user U] [--prefix P] [--dry-run] [--no-drop-in]
uninstall [--prefix P] [--dry-run] [--keep-count N] [--keep-days D] [--runs-dir D]
audit     [--prefix P] [--json] [--user U]
retention [--apply] [--runs-dir D] [--keep-count N] [--keep-days D] [--dry-run]
plane parse-agent-id [--in F]                                      # bunker test-plane evidence CLI
plane record --file F --step S --status S [--detail TEXT]
plane selftest-verdict --exit-code N
plane fold --file F
selftest  --all | --primitive <id> [--dir D] [--catalog-dir D]   # L0 scratch land+revert proofs; exit 0 only green-or-skip
version                      # stamped git sha (tag on release builds)
```

Safety floor (every landing path): no default target; protected targets refuse at
resolution; sanction-marker gate (SPEC-13) fails closed; load gate on landings.

## Arming + serving a hold (the loop from the CLI — verified live 2026-10-16)

```sh
# closed vocabulary, validated per-slot at ARM time (MISCHIEF-DF-29 and its
# 29-R2 re-cur, both closed; verified live on HEAD 8e8fd4f 2026-10-20):
#   inverse: restore-file (params path,backup) | remove-file (param path)
#   check:   file-matches-backup (params path,backup) | file-absent (param path)
mkdir -p /tmp/mc-scratch/{runs,target}
printf 'original\n' > /tmp/mc-scratch/target/config.toml
cp /tmp/mc-scratch/target/config.toml /tmp/mc-scratch/target/config.toml.bak
MISCHIEF_SANCTION=1 MISCHIEF_SANCTION_FILE=/tmp/mc-scratch/marker bin/mischief arm \
  --fault S-001 --target scratch:/tmp/mc-scratch/target --ttl 30s \
  --inverse-kind restore-file \
  --inverse-params path=/tmp/mc-scratch/target/config.toml,backup=/tmp/mc-scratch/target/config.toml.bak \
  --check-kind file-matches-backup \
  --check-params path=/tmp/mc-scratch/target/config.toml,backup=/tmp/mc-scratch/target/config.toml.bak \
  --dir /tmp/mc-scratch/runs          # marker file must carry a reason line
bin/mischief status --dir /tmp/mc-scratch/runs    # armed + ttl_remaining
# LAND the fault yourself (arm writes the hold only; printf FAULTED > file, etc.)
bin/mischief serve --dir /tmp/mc-scratch/runs     # watchdog: TTL fold + boot reconcile + health.json
# ORDER NO LONGER MATTERS (MISCHIEF-DF-30 fixed; re-proven live 2026-10-20 on 337ebe5):
# a hold armed while serve is ALREADY running IS folded and reverted at TTL — an arm
# issued ~0.8s after serve start was reverted on the next fold tick. Do not restart serve
# around every arm.
# THE FOLD IS A ~5s TICK (measured; MISCHIEF-DF-59 fixed): the revert lands at declared-TTL + up
# to one tick (+4.2s measured on BOTH a 5s and a 10s TTL) and can land ~0.7s EARLY (the
# expiry clock truncates the arm timestamp to whole seconds). Never conclude "the watchdog
# never reverted" from a window shorter than ttl + 5s — the product was right and the
# window was wrong. `status` keeps showing the hold as `armed` with a NEGATIVE
# ttl_remaining until the tick fires.
bin/mischief revert --all --dir /tmp/mc-scratch/runs   # kill switch (AC-6, ms-scale)
```

The journal is the evidence: each arm is one JSONL line with the full hold
(fault_id, target, inverse, check, ttl in ns, boot_id, arm_pid); each revert
writes a `revert_proof` line (`outcome` + `reason` + `detail=check: same` /
`check: absent` / the inverse's failure). Hold ids are content hashes — the same
declaration armed twice yields the same id; do not dedupe the journal.

## Recovering a revert that FAILED (`revert --hold <id> --retry`) — verified live 2026-10-20

A hold whose measured revert graded `revert_failed` says the fault may still be live. Before DF-52 it was a
dead end (every verb refused it; the only recovery was a fresh arm). Now:

```sh
bin/mischief revert --hold <id>            # rc=1, "… (already terminal: revert_failed — re-run with --retry …)"
bin/mischief revert --hold <id> --retry    # ONE more MEASURED attempt: rc=0 only when it grades reverted
```

* Admission is narrow on purpose: **only** `revert_failed`. A live (`armed`/`stuck`) hold belongs to the
  plain path and a `reverted` hold has nothing left to undo — both refuse with the status named, nothing
  written. `--retry` requires `--hold` and is mutually exclusive with `--all` (both rc=2).
* The journal is append-only: the failed attempt and every retry stay on the record (`reason=direct` for
  the first, `reason=retry` after), and `status` labels the hold with the LATEST outcome. `proofs: N` in
  `status` therefore counts attempts, not holds.
* A retry that fails again exits 1 and leaves the hold `revert_failed` — so retrying is idempotent: heal
  the cause (e.g. restore the inverse's backup file) and retry again.
* Creating a failed revert deliberately: arm first (arm pre-checks that the inverse's backup is readable),
  then move/delete the backup. Heal with `mv` **plus `chmod 644`** — mode 000 survives the move and yields
  `permission denied`, which reads like a product bug and is a probe bug.

## Gating you WILL hit outside a sanctioned host

- `selftest` is listed by the shipped top-level help: `cmd/mischief/main.go`
  renders it in `mischief --help`, and
  `cmd/mischief/help_test.go::TestTopLevelHelpListsSelftest` asserts the row at
  verified landing commit `b639bc8e903a379758fcd1ec95314f316c9dc419`. Re-run
  `go run ./cmd/mischief --help` and
  `go test -count=1 ./cmd/mischief/ -run TestTopLevelHelpListsSelftest -v`
  to verify the listing and its regression test; `mischief selftest --help`
  renders its flags and exits 0. It remains gated outside a sanctioned host and
  refuses on the SPEC-13 gate, rc=2. Same for `battery`, `plan` and `arm`:
  SPEC-13 wants a readable `/etc/mischief-sanction` (override via
  `MISCHIEF_SANCTION_FILE`, must carry a reason line) or `MISCHIEF_SANCTION=1`.
  On refusal it prints `rails: host_not_sanctioned: … nothing was written` —
  fail-closed, not a crash. Land faults only on an ephemeral sanctioned host
  (a bunker instance), never the main fleet host.

ch:trace row=DOC-21 spec=docs/prd/mischief-v0.1.md#61-verbs wave=mischief-foreman-2026-10-24-14-52-45#DOC-21 test=cmd/mischief/help_test.go::TestTopLevelHelpListsSelftest evidence=skills/mischief-usage/SKILL.md witness=tag:b639bc8@origin verdict=none:pending commit=b639bc8e903a379758fcd1ec95314f316c9dc419 memory=none:not-written
- **Sanction vars leak through long-lived agent shells.** A session that exported
  `MISCHIEF_SANCTION=1` for an earlier probe keeps it exported forever — a later
  "should refuse" leg silently succeeds. Probe refusals from a sanitized env
  (`env -u MISCHIEF_SANCTION -u MISCHIEF_SANCTION_FILE …`) and treat an
  unexpected SUCCESS on a should-refuse leg as env suspicion, not a gate bypass.
- Tarball-only installs: `doctor` needs the in-repo `catalog/faults` directory
  (resolved from the cwd upward; point `--catalog-dir` at one) and exits 1 without it.
- The shim `.so` is loaded from exactly one prebuilt location:
  `/tmp/mischief-shim/libfault-anchored.so`. Absent → mischief compiles the shim
  itself from source embedded in the binary (`cc -shared -fPIC -O2 … -ldl`).
  No prebuilt `.so` AND no compiler → `capability_unavailable`, never an unfaulted run.

## What exists

- `docs/prd/mischief-v0.1.md` — front door. Live-file claims about the fleet
  (the harness's bunker-qa.sh chaos cells) were verified 2026-10-02.
- `docs/FAULT-CATALOG.md` — design-only catalogue: 113 fault-id rows and 104 primitives across 8 layers
  (P14 S14 R12 F16 N18 C12 I14 T4) plus a 9-row simulated-case table under layer I
  (MSF-010: one runnable sim case per sim-covered I id, landed-proof written to the
  per-run request log). The shipped loader serves exactly the 11 ids in
  `catalog/faults/*.yaml`; ids outside that shipped boundary refuse at rc=2 with
  "not in the selftest corpus". This does not claim the product serves the design count.
  Counts verified exactly 2026-10-09 (recompute, never eyeball).
- `docs/SPEC-PLAN.md` — SPEC-01..12, each must falsify the PRD + carry NOT-LIST.
- `docs/` also carries: `RELEASE.md` (release runbook), `CATALOG.md` (schema fields),
  `BATTERY-PARITY.md`, `CHAOS-LANE.md`, `LANES.md`, `TEST-PLANE.md`, `EXPERIMENTS.md`,
  plus `.html` renders of the big three.
- `docs/dogfood/` — integration reports per dogfood run; `diagnostics.md` explains
  the write-ahead design and every arc (the 2026-10-16 entry closes the DF-05
  no-CLI-door arc).
- `probe/` — the measured capability audit. `capability-probe.sh` runs in ~0.3s,
  exit 0, 13 sections incl. `== freezer ==`. RESULTS.md's transcript now carries
  its provenance stamp (MSF-024, closed): it is the verbatim stdout of
  `capability-probe.sh @ 09eaff0b8738` — if the script is edited the stamp goes
  stale and the live output is authoritative again; re-run and diff.
- `catalog/faults/*.yaml` (10) — each carries all five contract fields; maturity is
  declared per row (mostly `L1-only` — a ladder state, not breakage).
- `catalog/experiments/*.yaml` (4) — rehearsal artifacts; `mischief plan -f` is now
  the selector-resolution entry point (fail-closed rails resolve targets or refuse).

## Raw capability demos (the probe still works standalone)

### Flagship fault (S-001 write-EIO, raw LD_PRELOAD fallback)

```bash
cd probe
D=$(mktemp -d); cp libfault-probe.c victim.c "$D"/; cd "$D"
cc -shared -fPIC -O2 -o libfault-probe.so libfault-probe.c -ldl && cc -O2 -o victim victim.c
MCFAULT="write:v.txt:EIO" LD_PRELOAD="$PWD/libfault-probe.so" ./victim "$PWD/v.txt"
# -> write(...v.txt) -> -1 errno=Input/output error ; target file stays 0 bytes
cat /tmp/mc-fault-hits.<pid>   # LANDED-PROOF: hits=1 rule=... — written by the
                               # shim, hardcoded under /tmp, NOT the cwd
MCFAULT="write:v.txt:EIO:1"    # bounded: exactly one hit
```

Inverse: run victim again without MCFAULT — writes succeed.
(The shipped CLI wraps this same shim — see `/tmp/mischief-shim/` above.)

### Network substrate (needs sudo, the L2 path)

```bash
sudo ip netns add mc-probe-ns && sudo ip netns exec mc-probe-ns \
  tc qdisc add dev lo root netem loss 100%
sudo ip netns exec mc-probe-ns ping -c1 127.0.0.1  # rc=2 = fault landed
sudo ip netns del mc-probe-ns                      # revert proven
```

## Pitfalls

- The probe's landed-proof counter path is `/tmp/mc-fault-hits.<pid>` (constant in
  the shim source), not the working directory — the RESULTS.md layout suggests cwd.
- Don't read `maturity: L1-only` on catalog rows as broken — it is the ladder state
  (landed on this host, no battery cell on shared targets yet).
- Board rows are MSF-<n> in `.coding-hermes/board/tasks.jsonl` (v2.1 JSONL,
  boardctl-managed). Design rows MSF-001..022 pre-date the dogfood run.
- "Selectors have no resolution semantics" (old MSF-026, closed) is SUPERSEDED:
  `plan` resolves targets through fail-closed rails; unsanctioned hosts refuse
  before anything is written.
- `arm` validates inverse/check KINDS at arm time against the kind's own slot
  (MISCHIEF-DF-29 and the 29-R2 re-cur, both closed): an unknown kind — or a
  kind from the wrong slot — refuses rc=2 naming the field, nothing written.
  Stick to the two kinds per decl above; anything else is a usage refusal,
  not a journalable arm.
- `serve`'s `boot_id` (what `status` prints and what arm records carry) comes from the
  run dir's `boot.json`, not the host's `/proc/sys/kernel/random/boot_id` — an arm record
  stamped with the host id grades false-stuck on the same boot. That run-dir value is
  **run-dir-scoped**: it stays constant across serve restarts (observed 2026-10-20:
  `boot.json.reverter_boot` unchanged while three daemons started and died).
  `health.json`'s `daemon_boot` is a DIFFERENT field and is **unique per serve process
  start** since MISCHIEF-DF-32 was fixed (re-verified live 2026-10-20 on 337ebe5: three
  consecutive `serve` starts → `5d387c00bc1afead`, `55e5526bb5b1b018`, `8038b874f5edd71f`),
  so it is a usable freshness key for "has this daemon restarted". Host boot is
  `boot.json.host_boot`.
- A long-running `serve` NOW folds the holds it did not load at boot — MISCHIEF-DF-30 is
  fixed and re-proven live 2026-10-20 (arms issued after the daemon started reverted at
  TTL). The `boot_id` scope caveat in MISCHIEF-DF-30's row text (since fixed) is stale:
  `status` and `revert --all` still fold fresh and remain the authoritative read, but
  `health.json` is no longer blind to post-boot arms (its `holds`/`reverted` counters
  tracked the arms in this run's probes).
- `arm`'s write-ahead line now reads "serve reverts at TTL" (MISCHIEF-DF-31,
  closed): the once-advertised Detach route was removed from the message
  entirely, so there is no second containment path to count on — `serve` and
  `revert --all` are the only TTL owners.
- `status` (default) lists ACTIVE holds only (armed/stuck) since DF-54 — a **failed** revert is terminal,
  so it is visible only in the `proofs:` list, in `status --all`, or in `health.json`. With a fault still
  landed and its revert failed, `status` prints `holds: 0` and exits **0** (MISCHIEF-DF-54-R2) while the
  daemon on the same run dir says `ok:false / revert_failed:1`: do not gate a script on `status`'s exit code
  to mean "host is clean", count `revert_failed` yourself (`status --json | jq '[.history[]|select(.outcome=="revert_failed")]|length'`).
- The kill switch's **per-hold stdout line** always starts with the word `reverted`, whatever the outcome
  (`reverted <id> outcome=revert_failed …`, main.go:365) — the exit code and the summary line are honest
  since DF-51, the leading word is not (MISCHIEF-DF-51-R2, closed). Parse `outcome=`, not the verb.
  <!-- ch:trace row=MISCHIEF-DF-63-FIXUP spec=tools/check_row_drift.py#current-status test=tools/check_row_drift.py doc=skills/mischief-usage/SKILL.md prompt=/tmp/mischief-MISCHIEF-DF-63-FIXUP-brief.txt evidence=commit witness=none:docs-regression verdict=none:pending-foreman-judge commit=none:pending-foreman-trace memory=none:not-written -->
 - `status --json` in the healthy state renders `"holds": null` (not `[]`), so `jq '.holds[]'` fails while
  `.holds|length` is 0; and a terminal hold keeps its arm-time `expiry` on the wire, so `expiry - now` is
  POSITIVE for a hold reverted before its TTL (MISCHIEF-DF-54-R2, resolved). Gate on `.status`, never on the expiry.
- **`arm` NOW evaluates the protected-target rail** (MISCHIEF-DF-58, closed by f794ed0/37310b3; re-verified
  live 2026-10-20 on HEAD 8e8fd4f). `arm --target process:scheduler` refuses rc=2 `protected target
  "scheduler" refused` and writes NOTHING — no run dir, no hold — the same verdict `plan` gives for the
  same selector, and `status --dir <D>` then reads `holds: 0`. The match is deliberately prefix-wide, so
  near-miss names refuse as well: `process:schedulerd`, `process:hermes-scheduler`, `process:gateway-test`,
  `process:scheduler-probe`, `host:memory-daemon` all refuse (intended over-refusal in
  `rails.selectorNames`), while unrelated names (`process:hermes-api`, `process:loadbalancer`,
  `container:mischief-scratch`) arm normally rc=0. Because the remediation the refusal names does not
  exist (next bullet), a legitimately-named `…-gateway-…` workload is not faultable at all in v0.1.
The protected-target refusal is fail-closed and does not expose an override flag in v0.1 (MISCHIEF-DF-57 is closed).
  (`plan`: `-catalog-dir -f -journal-dir -json -plan-only-sanction-skip -scratch-dir`; `arm`: `-catalog-dir
  -check-kind -check-params -dir -fault -inverse-kind -inverse-params -target -ttl`) and no doc names one.
  A protected target is therefore not faultable at all in v0.1, despite the message.
- An **absent** run dir is a named refusal (MISCHIEF-DF-60, closed by d51d65e/72d3939; re-verified live
  2026-10-20 on HEAD 8e8fd4f): `revert --all --dir <nonexistent>` prints `revert: run dir <D> absent
  (nothing to revert)` on stderr, rc=1, with NO `kill switch:` line and creates nothing; `status --dir
  <nonexistent>` prints `run_dir: <D>` + `state: absent`, rc=1, and mints no `boot_id`; a present (even
  empty) dir still answers `kill switch: 0 holds reverted, wall=0s` rc=0. Two adjacent states to know:
  a path that is a FILE, an unreadable dir, or `/root/…` falls through to the store and reports
  `reverter: state dir: mkdir …: not a directory|permission denied` (rc=1, stderr, nothing written); and
- `status --json` over an absent dir now emits one parseable JSON object (`{"run_dir":"...","state":"absent"}`)
  so `status --json | jq .state` returns `absent` (MISCHIEF-DF-61, closed by 733208e).
- The torn-journal ladder below was measured on 2026-10-17; DF-39-R2 / DF-40 / DF-41 are all closed rows
  now and the ladder has NOT been re-measured since. Re-probe before quoting it.

## Privileged lifecycle verbs (`install`, `uninstall`, `retention`) — apply-leg contract

Verified by real use 2026-10-16 (lane mischief-chaos-dogfood; findings
MISCHIEF-DF-33/34/35, all closed):

- `--dry-run` prints the exact plan the apply executes (install: 2 actions planned
  == 2 applied; uninstall: 3 planned, present artefacts removed). Use it for
  change review — but it is NOT evidence that the apply works. The rotation half
  of `uninstall` failed on every real run dir on the same day its `--dry-run`
  read clean.
- Rotate run history with `mischief retention --runs-dir D --keep-count N --apply`
  (works: non-empty dirs removed, rc=0). `uninstall`'s integrated rotation now
  handles real run dirs (MISCHIEF-DF-33 closed) and lifecycle verbs report
  per-step state when a step fails (MISCHIEF-DF-34 closed) — a failed step no
  longer leaves you guessing what was already deleted. Empty fixture dirs are
  still a useless rotation test: exercise rotation against a real run dir shape.
- No keep bounds at all -> `--runs-dir` is silently ignored (artefacts removed,
  rotation skipped, rc=0). `--keep-count 0` is read as UNSET and refuses rc=2
  ("both keep-count and keep-age are unset"): express "keep none" as
  `--keep-count 1` plus a delete, never 0.
- Unprivileged `install` WITHOUT `--no-drop-in` prints the `/etc/sudoers.d`
  ADVISORY and then attempts that write anyway (rc=1 `permission denied`),
  leaving the freshly `mkdir`ed prefix dir behind. `--no-drop-in` is the
  unprivileged path and it really does install the manifest (the README calls it
  a "manifest-only plan" — it is not one).
- After a manifest-only install, `audit` grades `condition: manifest_only`
  (DF-35 fixed: previously it wrongly reported `not_installed`) — still exit 0
  (healthy: the drop-in write needs root), but a distinct token a script can
  key on to tell "manifest landed, drop-in pending" from a never-installed
  host.

## Driving the RELEASED artifact (README Option A) — verified on a fresh agent 2026-10-16

The tarball path (no Go toolchain) is real and honest — the README-side
frictions MISCHIEF-DF-36/37/38 (all closed) left two lasting rules of thumb:

- `install -m 0755 mischief ~/.local/bin/` fails rc=1 if `~/.local/bin` does
  not exist (a bare Debian user has none). Rescue: `mkdir -p ~/.local/bin`
  first, or use `install -Dm 0755` (GNU -D creates the dir) — the README's
  Install section now leads with the mkdir.
- `plan` and catalog-backed verbs need the `catalog/` tree from a checkout
  (or `--catalog-dir`); the tarball ships none — `doctor` names the in-repo
  `catalog/faults` requirement and exits 1 without it.
- Matching a newer catalog to the older release binary turns `selftest --all`
  red with `unknown primitive "F-008": not in the selftest corpus` and NO
  version-skew hint. With the tag's own catalog (`git archive v0.1.0 catalog`
  — 10 fault files, not HEAD's 11) the SAME binary passes rc=0. When selftest
  fails with that message, check catalog vintage before suspecting the product.
- `plan -json` on the release prints PascalCase keys (`RunID`, `TTLRaw`,
  `ExperimentID`, no `scratch_dir`); HEAD prints snake_case with
  `scratch_dir` (the DF-26 contract). Scripts against released tarballs must
  parse the PascalCase shape until the v0.1.1 cut.
- Raw download→verify→unpack→smoke timings on bare agents: 0.5-3s each step,
  plan 0.01s, selftest 0.53-0.65s cold — the mechanical path is fast; the only
  stalls are the three README gaps above.

## When the reverter journal is corrupt — three failure modes (verified 2026-10-17)

`serve` is the TTL owner, so the one state it must handle honestly is a damaged
`<run>/reverter.jsonl`. As of HEAD c28800d the DF-39 crash is gone but the
degraded leg is split three ways — full ladder in
`docs/dogfood/2026-10-17-torn-journal-ladder.md`:

- Torn line appended WHILE the daemon runs (MISCHIEF-DF-39-R2, closed): as
  measured 2026-10-17 (re-probe before quoting), the daemon stayed up but the
  tick loop `continue`d before `writeHealth`, so `health.json` kept the last
  good document — `ok:true`, TTLs decaying, no `refold_error` — and no TTL
  revert was attempted while the corruption was there (target stays FAULTED
  past its TTL, zero proofs). Repairing the journal makes the SAME daemon fold
  clean on its next beat and revert normally: no restart needed. Treat
  `ok:true` as unproven whenever you cannot explain the health file's
  `generated_at`; `mischief status --dir <run>` is the cross-check (a
  separate process, failing closed rc=1 with `journal fold: ... unexpected EOF`).
- Torn line present BEFORE the daemon starts (MISCHIEF-DF-40, closed by
  361b64d): pre-fix (measured 2026-10-17) `serve` panicked on a nil
  `*reverter.Store` and `health.json` was never created — there was nothing to
  read at all. Since 361b64d the daemon starts DEGRADED on a torn boot journal
  and publishes `ok:false` + `refold_error` on the tick path; CLI verbs still
  refuse fail-closed. Repair the journal first either way.
- Torn journal + a stop request (MISCHIEF-DF-41, closed): as measured
  2026-10-17 (re-probe before quoting), SIGTERM/SIGINT self-deadlocked in the
  ctx-cancel final beat (`render` re-locks `d.mu` while `writeHealth` holds
  it). The process ignored the signal — use `kill -9` and repair the journal;
  do not wait for a clean exit.
- The sanction variable is `MISCHIEF_SANCTION` (no "F"): a transposed
  `MISFCHIEF_SANCTION=1` produces the same `host_not_sanctioned` refusal rc=2 as
  setting nothing at all, so read the refusal text before blaming the host.

ch:trace row=DOC-22 spec=none:docs-row wave=mischief-foreman-2026-10-28-19-31-37.json#DOC-22 test=none:doctor-wc-greps doc=skills/mischief-usage/SKILL.md prompt=/tmp/mf-20261028-1931-DOC-22-brief.txt evidence=commit witness=none:doctor-output verdict=none:pending-foreman-judge commit=self memory=none:board-row
STATUS: complete
TIER: T1 (edit + doctor/wc/grep checks invoked and observed)
EVIDENCE: commit
