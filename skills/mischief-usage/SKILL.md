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

## The CLI (v0.1.0 verbs — verified against `./bin/mischief --help`, 2026-10-09)

```text
plan      -f <exp.yaml> [--json] [--scratch-dir D]          # zero side effects (AC-1)
status    [--dir D]           # folds the run journal: armed/held holds, stuck, proofs
revert    (--all | --hold <id>) [--dir D]                   # `revert --all` = the kill switch
doctor    [--catalog-dir D] [--json] [--self-check]         # exit 1 = a check FAILED
battery   [--quick] [--project P] [--primitive ID] [--out D] [--file-rows] [--dry-run]
chaos     matrix --project P [--tier-max N] [--out D]
        | check --lane F
        | finding --project P --board D --title T --reason R
sim       --shape I-00N [--path P] [--log F] [--allow-real --resource TAG --spend-cap USD]
serve     --dir D             # reverter daemon: TTLs + boot reconcile + health.json
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

## Gating you WILL hit outside a sanctioned host

- `selftest` is real but gated, so `--help` does not list it (verified 2026-10-09:
  it runs and refuses on the SPEC-13 gate, rc=2). Same for `battery` and `plan`:
  SPEC-13 wants a readable `/etc/mischief-sanction` (override via
  `MISCHIEF_SANCTION_FILE`, must carry a reason line) or `MISCHIEF_SANCTION=1`.
  On refusal it prints `rails: host_not_sanctioned: … nothing was written` —
  fail-closed, not a crash. Land faults only on an ephemeral sanctioned host
  (a bunker instance), never the main fleet host.
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
