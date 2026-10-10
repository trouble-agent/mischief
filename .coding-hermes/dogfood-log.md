# mischief dogfood log

## 2026-10-02 — mischief-dogfood lane (first dogfood run)

- Verdict: PROMISING-BUT-ROUGH (per-surface: design/docs SHIPPABLE-as-design,
  experimental artifacts ROUGH)
- Promise: "fault injection for a fleet of Linux daemons… no code yet" — so the
  design artifacts ARE the product; a real user verifies the design's claims and
  consumes the catalog/YAMLs.
- Time-to-first-success: ~4 min (capability probe re-run, all claims held);
  S-001 landed with independent proof ~15 min in, first try.
- Top findings: MSF-024 RESULTS.md transcript stale vs probe script (P1);
  MSF-025 five primitives self-grade maturity:proven with no selftest possible
  (P1); MSF-026 experiment selector dialects undefined (P2); MSF-027 build
  warnings (P3); MSF-023 explicit SKIPPED-install-bunker (nothing installable).
- Verified live: probe script 0.31s exit 0; S-001 EIO + landed-proof counter +
  bounded budget + inverse; netns/netem partition land+revert; seccomp
  user-notify; rootless scopes; catalog 104/104 counts, no dups/gaps.
- Perf: nothing user-noticeable (probe 0.31s, build 5.2s, fault <50ms) — no PERF
  rows filed, deliberately.
- Artifacts: docs/dogfood/2026-10-02-integration.md, docs/dogfood/diagnostics.md,
  skills/mischief-usage/SKILL.md. Foreman not woken (cooldown lane, rows filed —
  board-driven admission picks them up).
## 2026-10-08 — mischief-chaos dogfood (release-install surface)

- Verdict: SHIPPABLE — first real install verdict on v0.1.0, filed yesterday by RELEASE-003.
- Angle: the surface the 2026-10-02 run could not touch. That run predated the first cut
  and dogfooded a design-stage repo; this one starts from the actual published artifact.
- Promise tested: "a fresh user can install and run mischief v0.1.0 from the GitHub Release."
- Real use: gh release download v0.1.0 (DL 1s) -> sha256sum -c checksums.txt (both OK) ->
  untar amd64 -> binary from scratch dir: version (3ms), doctor, status, revert --all
  (kill switch, 0 holds, wall 0s), plan (sanction refusal, exit 2), selftest --all
  (sanction refusal, exit 2), install --dry-run (3 actions, exit 0). All as a naive user
  with zero env setup. Binary is fully standalone; refusals are the best UX in the fleet.
- Bunker leg: SKIPPED-install-bunker (explicit) — bunker3/100.69.3.13:22 connect timeout
  on two probes; no clean-machine install proof this tick. Filed MSF-034.
- Findings: MSF-033 (P1: NO consumer install path — released README points at make bin +
  repo files not in the archive), MSF-034 (P2: SKIPPED-install-bunker), MSF-035 (P3:
  doctor prints 'catalog load FAILED' then exits 0 — failed check graded success).
- Perf (Step 2b): nothing a user would notice — every op <=5ms cold; no PERF rows, deliberately.
- Foreman not woken (cooldown lane; board-driven admission picks the rows up).

## 2026-10-10 — mischief-chaos dogfood (bunker install closure + exit-contract re-verify)

- Verdict: SHIPPABLE — the leg the 2026-10-08 run could not run (SKIPPED-install-bunker,
  MSF-034) is now PROVEN on a fresh ephemeral agent; exit contracts on the released
  binary are all correct.
- Angle: the skipped install leg, re-driven after ~15 new commits landed (docs audit
  DOC-6..10, README-3..6, SEC-001 toolchain bump).
- Promise tested: "a fresh user can install and run mischief v0.1.0 from the GitHub
  Release on a clean machine" — this time actually on one.
- Real use: bunker-las-02 agent 0c101d00 (Debian 13, bare user, NO gh — used the README's
  curl fallback). curl download+verify+unpack=2s; install to ~/.local/bin (needed mkdir
  first); version 3ms; status 5-7ms exit 0; revert --all exit 0 (kill switch 0 holds);
  plan refusal exit 2; selftest sanction refusal exit 2; install --dry-run 3 actions
  exit 0; doctor exit 1 on catalog failure (MSF-035 fix confirmed on the RELEASED binary,
  not just HEAD). selftest --all w/ sanction+staged catalog: 13 pass / 8 skip / 0 fail (21).
  All four shipped experiments driven via plan: ex-004 plans clean; ex-001 refs F-008
  not in the shipped catalog (empty-field plan payload, DF-01); ex-002 refused
  scope_below_minimum (L0 vs L3, correct); ex-003 refs P-006 (also unshipped). Neither
  F-008 nor P-006 ships in catalog/faults — the repo's own examples are un-runnable
  without authoring faults, DF-02. Agent destroyed (bunker destroy OK).
- Perf (Step 2b): nothing a user would notice — plan warm 6-8ms, status 5-7ms cold,
  install 2s; no PERF rows, deliberately.
- Findings filed: DF-01 (P2 unknown-primitive plan payload), DF-02 (P2 quickstart
  can't plan any shipped experiment; sanction var undisclosed), DF-03 (P3 closes
  MSF-034's open leg), DF-04 (P3 MSF-035 premise false on released binary — correction row).
- Foreman not woken (board-driven admission picks the rows up).
