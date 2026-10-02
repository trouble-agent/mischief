# mischief dogfood integration report — 2026-10-02

Run: mischief-dogfood lane (skill `coding-hermes-dogfood`), ~45 min hands-on.
Verdict: **PROMISING-BUT-ROUGH** — per-surface: design+docs SHIPPABLE-as-design /
experimental artifacts ROUGH (see findings MSF-024..027).

## What the project promises

"No code yet" (README, verbatim). The product at this stage IS the design:
a fault-injection contract (every fault = landed-proof + inverse + capability),
a 104-primitive catalog across 8 layers, a spec set (SPEC-01..12), and a measured
capability audit (probe/RESULTS.md) the design rests on.

## How this run used it for real

A real user of a design-stage project is (a) a future spec implementer and
(b) someone verifying the design's factual claims. Both were done:

1. **Re-ran the documented capability probe** (`probe/capability-probe.sh`, 0.3s,
   exit 0) and re-ran every hand-run section RESULTS.md shows: netns + netem
   100% loss partition landed (ping rc=2) and reverted (`netns_del=OK`);
   LD_PRELOAD shim + victim compiled and run; seccomp user-notify
   (`notif=80 resp=24`); rootless scope (`systemd-run --user -p MemoryMax=64M ... true`)
   and the `systemctl --user freeze` verb. **All substrate claims held.**
2. **Landed S-001 (write-EIO) end-to-end**, the flagship rootless primitive, in a
   scratch dir: EIO on the named path, 0-byte target state, unaffected fd kept
   working, bounded budget (`:1`) exactly one hit, and the **independent
   landed-proof counter** (`/tmp/mc-fault-hits.<pid>` → `hits=1 rule=...`)
   written by the shim itself. Inverse verified: a fresh run without the rule
   writes cleanly. The proof contract — the project's core idea — demonstrably
   works on this host.
3. **Audited the catalog contract**: recomputed the catalog's numeric claims
   programmatically — 104 rows, per-layer counts (P14 S14 R12 F16 N18 C12 I14 T4)
   all match, zero duplicate ids, zero numbering gaps. All 10 shipped fault YAMLs
   carry all five contract fields.
4. **Consumed the experiment YAMLs** as a spec implementer would — found the
   selector dialect undefined (MSF-026).

## What worked (evidence)

- probe build: `cc -shared -fPIC -O2 -o libfault-probe.so libfault-probe.c -ldl`
  + victim + seccomp-probe ≈ 5s wall, zero errors (one -Wformat warning, MSF-027).
- S-001 landed-proof is genuinely independent of the target — the counter file
  exists even if the victim lies. This is the differentiator vs the legacy
  bunker-qa chaos cells, and it held under real use.
- The PRD's live-file claims check out: 5 chaos- cells (incl. chaos-probes) still
  present in ~/.hermes/scripts/bunker-qa.sh (69 line-hits).

## What did not (→ board rows)

- MSF-024 (P1): RESULTS.md transcript stale vs the script (13 vs 12 sections,
  new freezer line, per-tool paths) — doc promises "every line is re-runnable".
- MSF-025 (P1): 5 primitives self-graded `maturity: proven` though no selftest
  can exist yet (no binary). The maturity ladder is the project's honesty
  mechanism; it must not be pre-spent.
- MSF-026 (P2): four experiment YAMLs use four selector dialects, undefined
  anywhere; placeholders (`<state_root>`, `<key>`) unreplaced.
- MSF-027 (P3): seccomp-probe.c printf %zu/int warnings.
- MSF-023 (P3): explicit SKIPPED-install-bunker record — nothing installable exists.

## Perf

Nothing here is slow enough to be a finding: probe 0.31s, build 5.2s, S-001
land+prove < 50ms. No PERF rows filed — a win nobody can feel is not a finding.

## Re-run recipe for the next agent

```bash
cd probe && bash capability-probe.sh          # 13 sections, exit 0
D=$(mktemp -d); cp libfault-probe.c victim.c "$D"/; cd "$D"
cc -shared -fPIC -O2 -o libfault-probe.so libfault-probe.c -ldl && cc -O2 -o victim victim.c
MCFAULT="write:v.txt:EIO" LD_PRELOAD="$PWD/libfault-probe.so" ./victim "$PWD/v.txt"
cat /tmp/mc-fault-hits.*    # <- landed-proof, independent of the target
```
