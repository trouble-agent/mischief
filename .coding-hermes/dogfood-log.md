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
