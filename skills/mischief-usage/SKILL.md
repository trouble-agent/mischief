---
name: mischief-usage
description: Use when working with or on the mischief repo (fault-injection design project) — what exists, what runs, how to verify claims.
---

# mischief — usage for agents

mischief is a **design-stage** fault-injection project ("faults are data":
landed-proof + inverse + blast radius). There is NO binary. Do not look for a
CLI to run; the usable surfaces are the catalog, the probe, and the YAMLs.

## What exists

- `docs/prd/mischief-v0.1.md` — front door. Live-file claims about the fleet
  (the harness's bunker-qa.sh chaos cells) were verified 2026-10-02.
- `docs/FAULT-CATALOG.md` — 104 primitives, 8 layers (P14 S14 R12 F16 N18 C12
  I14 T4). Counts verified exactly 2026-10-02 (recompute, never eyeball).
- `docs/SPEC-PLAN.md` — SPEC-01..12, each must falsify the PRD + carry NOT-LIST.
- `probe/` — the measured capability audit. `capability-probe.sh` runs in ~0.3s,
  exit 0. NOTE: RESULTS.md transcript is stale vs the script (MSF-024) — trust
  the live output, 13 sections incl. `== freezer ==`.
- `catalog/faults/*.yaml` (10) — each carries all five contract fields.
- `catalog/experiments/*.yaml` (4) — rehearsal artifacts; selectors
  (`container:trouble-hub`, `host:.`, `process:troubled`,
  `process:hermes-gateway`) have NO resolution semantics yet (MSF-026). Don't
  try to execute them.

## How to run the flagship fault (S-001 write-EIO, works today)

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

## Network substrate (needs sudo, the L2 path)

```bash
sudo ip netns add mc-probe-ns && sudo ip netns exec mc-probe-ns \
  tc qdisc add dev lo root netem loss 100%
sudo ip netns exec mc-probe-ns ping -c1 127.0.0.1  # rc=2 = fault landed
sudo ip netns del mc-probe-ns                      # revert proven
```

## Pitfalls

- The landed-proof counter path is `/tmp/mc-fault-hits.<pid>` (constant in the
  shim source), not the working directory — the RESULTS.md layout suggests cwd.
- `maturity: proven` on S-001/N-001/N-012/R-001/I-002 overstates the ladder
  (no selftest can exist pre-binary) — treat as hand-verified (MSF-025).
- Board rows are MSF-<n> in `.coding-hermes/board/tasks.jsonl` (v2.1 JSONL,
  boardctl-managed). Design rows MSF-001..022 pre-date the dogfood run.
- No install path exists: SPEC-11 owns install. A fresh-machine leg is not
  applicable until M1 (MSF-014) lands a binary.
