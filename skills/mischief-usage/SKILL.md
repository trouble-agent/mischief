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

## The CLI (v0.1.0 verbs — verified against `./bin/mischief --help`, 2026-10-09; arm verified live 2026-10-16)

```text
plan      -f <exp.yaml> [--json] [--scratch-dir D]          # zero side effects (AC-1)
status    [--dir D]           # folds the run journal: armed/held holds, stuck, proofs
revert    (--all | --hold <id>) [--dir D]                   # `revert --all` = the kill switch
arm       --fault ID --target SEL --ttl D --inverse-kind K --inverse-params k=v,.. --check-kind K --check-params k=v,.. [--dir D]
serve     --dir D             # reverter daemon: TTLs + boot reconcile + health.json
doctor    [--catalog-dir D] [--json] [--self-check]         # exit 1 = a check FAILED
battery   [--quick] [--project P] [--primitive ID] [--out D] [--file-rows] [--dry-run]
chaos     matrix --project P [--tier-max N] [--out D]
        | check --lane F
        | finding --project P --board D --title T --reason R
sim       --shape I-00N [--path P] [--log F] [--allow-real --resource TAG --spend-cap USD]
install   [--user U] [--prefix P] [--dry-run] [--no-drop-in]
uninstall [--prefix P] [--dry-run] [--keep-count N] [--keep-days D]
audit     [--prefix P] [--json]
retention [--apply] [--runs-dir D] [--keep-count N] [--keep-days D] [--dry-run]
plane     parse-agent-id | record | selftest-verdict | fold    # bunker test-plane evidence CLI
selftest  --all | --primitive <id>   # L0 scratch land+revert proofs; exit 0 only green-or-skip
version                      # stamped git sha (tag on release builds)
```

Safety floor (every landing path): no default target; protected targets refuse at
resolution; sanction-marker gate (SPEC-13) fails closed; load gate on landings.

## Arming + serving a hold (the loop from the CLI — verified live 2026-10-16)

```sh
# closed vocabulary, refuse-on-unknown at REVERT (not yet at arm — MISCHIEF-DF-29):
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
bin/mischief serve --dir /tmp/mc-scratch/runs     # watchdog: reverts at TTL, health.json
# ORDER MATTERS: arm BEFORE serve. A hold armed while serve is ALREADY running is
# invisible to that daemon (MISCHIEF-DF-30): it is never reverted at TTL, the fault
# stays landed, and health.json keeps reporting ok:true. Restart serve after arming
# (boot reconcile folds the journal) or use the kill switch.
bin/mischief revert --all --dir /tmp/mc-scratch/runs   # kill switch (AC-6, ms-scale)
```

The journal is the evidence: each arm is one JSONL line with the full hold
(fault_id, target, inverse, check, ttl in ns, boot_id, arm_pid); each revert
writes a `revert_proof` line (`outcome` + `reason` + `detail=check: same` /
`check: absent` / the inverse's failure). Hold ids are content hashes — the same
declaration armed twice yields the same id; do not dedupe the journal.

## Gating you WILL hit outside a sanctioned host

- `selftest` is real but gated, so `--help` does not list it (verified 2026-10-09:
  it runs and refuses on the SPEC-13 gate, rc=2). Same for `battery`, `plan` and
  `arm`: SPEC-13 wants a readable `/etc/mischief-sanction` (override via
  `MISCHIEF_SANCTION_FILE`, must carry a reason line) or `MISCHIEF_SANCTION=1`.
  On refusal it prints `rails: host_not_sanctioned: … nothing was written` —
  fail-closed, not a crash. Land faults only on an ephemeral sanctioned host
  (a bunker instance), never the main fleet host.
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
- `docs/FAULT-CATALOG.md` — 113 fault-id rows: 104 primitives across 8 layers
  (P14 S14 R12 F16 N18 C12 I14 T4) plus a 9-row simulated-case table under layer I
  (MSF-010: one runnable sim case per sim-covered I id, landed-proof written to the
  per-run request log). Counts verified exactly 2026-10-09 (recompute, never eyeball).
- `docs/SPEC-PLAN.md` — SPEC-01..12, each must falsify the PRD + carry NOT-LIST.
- `docs/` also carries: `RELEASE.md` (release runbook), `CATALOG.md` (schema fields),
  `BATTERY-PARITY.md`, `CHAOS-LANE.md`, `LANES.md`, `TEST-PLANE.md`, `EXPERIMENTS.md`,
  plus `.html` renders of the big three.
- `docs/dogfood/` — integration reports per dogfood run; `diagnostics.md` explains
  the write-ahead design and every arc (the 2026-10-16 entry closes the DF-05
  no-CLI-door arc).
- `probe/` — the measured capability audit. `capability-probe.sh` runs in ~0.3s,
  exit 0. NOTE: RESULTS.md transcript is stale vs the script (MSF-024) — trust
  the live output, 13 sections incl. `== freezer ==`.
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
- "Selectors have no resolution semantics" (old MSF-026) is SUPERSEDED: `plan`
  resolves targets through fail-closed rails; unsanctioned hosts refuse before
  anything is written.
- `arm` accepts unknown inverse/check KINDS (structure-only validation at arm
  time; the whitelist fires only at revert, MISCHIEF-DF-29). Stick to the two
  kinds per decl above until DF-29 closes — an invented kind journals fine and
  grades `revert_failed` at expiry.
- `serve`'s `boot_id` is the REVERTER's id (from the run dir's `boot.json`), not
  the host's `/proc/sys/kernel/random/boot_id` — an arm record stamped with the
  host id grades false-stuck on the same boot. The reverter id is itself
  HOST-BOOT-scoped: it is reused across daemon restarts (identical in health.json
  for three different `pid`s), so `daemon_boot` identifies the host generation,
  not the daemon process — use `pid`/`started_at`/`generated_at` for freshness
  (MISCHIEF-DF-32).
- A long-running `serve` folds only the holds it loaded at boot (MISCHIEF-DF-30):
  its revert loop and `health.json` never re-read the journal. Trust `status` and
  `revert --all` (both fold fresh) over `health.json` when asking "is anything
  armed past its TTL?".
- The detached-owner route `arm` names in its write-ahead line ("serve (or
  Detach) reverts at TTL") is NOT reachable from any verb: `Lifecycle.Detach` and
  the `__owner` entry exist but no flag/subcommand calls them (MISCHIEF-DF-31).
  Do not count on it as a second containment path.

## Privileged lifecycle verbs (`install`, `uninstall`, `retention`) — apply-leg contract

Verified by real use 2026-10-16 (lane mischief-chaos-dogfood; findings MISCHIEF-DF-33/34/35):

- `--dry-run` prints the exact plan the apply executes (install: 2 actions planned
  == 2 applied; uninstall: 3 planned, present artefacts removed). Use it for
  change review — but it is NOT evidence that the apply works. The rotation half
  of `uninstall` failed on every real run dir on the same day its `--dry-run`
  read clean.
- Rotate run history with `mischief retention --runs-dir D --keep-count N --apply`
  (works: non-empty dirs removed, rc=0). Do NOT use `uninstall`'s integrated
  rotation until DF-33 closes: `uninstall --runs-dir D --keep-count/--keep-days`
  dies `remove <dir>: directory not empty` (rc=1) on any run dir containing a
  journal file, rotates nothing, and by then has already deleted the helper
  manifest (DF-34). The one shape that passes is an EMPTY fixture dir — never
  test rotation with empty dirs.
- No keep bounds at all -> `--runs-dir` is silently ignored (artefacts removed,
  rotation skipped, rc=0). `--keep-count 0` is read as UNSET and refuses rc=2
  ("both keep-count and keep-age are unset"): express "keep none" as
  `--keep-count 1` plus a delete, never 0.
- Unprivileged `install` WITHOUT `--no-drop-in` prints the `/etc/sudoers.d`
  ADVISORY and then attempts that write anyway (rc=1 `permission denied`),
  leaving the freshly `mkdir`ed prefix dir behind. `--no-drop-in` is the
  unprivileged path and it really does install the manifest (the README calls it
  a "manifest-only plan" — it is not one).
- After a manifest-only install, `audit` still reports `condition: not_installed`
  while its own output says the manifest is installed and matching (DF-35): do
  not key a script on that condition, and expect `audit` to be exit 0 either way.

## Driving the RELEASED artifact (README Option A) — verified on a fresh agent 2026-10-16

The tarball path (no Go toolchain) is real and honest — but three frictions cost
a fresh consumer three stopped moments (MISCHIEF-DF-36/37/38, all README-side):

- `install -m 0755 mischief ~/.local/bin/` fails rc=1 if `~/.local/bin` does
  not exist (a bare Debian user has none). Rescue: `mkdir -p ~/.local/bin`
  first, or use `install -Dm 0755` (GNU -D creates the dir).
- `plan` and catalog-backed verbs need the `catalog/` tree from a checkout
  (or `--catalog-dir`); the tarball ships none and the README mentions this
  only as a `doctor` caveat.
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
