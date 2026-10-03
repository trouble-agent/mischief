// Package rails is SPEC-03: the refusal rails every fault must pass before a
// backend is allowed to touch a target — target resolution, the protected
// list, the scope ladder, the load gate — and the refusal taxonomy they all
// speak.
//
// Design authority: docs/SPEC-PLAN.md (SPEC-03 rails & blast) and
// docs/prd/mischief-v0.1.md §5 (the target is an argument, resolved
// explicit-argument → experiment-declared scope → refusal; there is no
// default target, ever) and §7 (the safety doctrine). The types this builds
// on are internal/catalog's (Tier.Min, SelftestState.RefusesOutsideL0) — the
// ladder and the selftest predicate are not redefined here.
//
// The four rails, in the order a run meets them:
//
//   - Target resolution (Resolve): no default target (AC-2); the protected
//     list and the structural exclusions are evaluated at resolution, before
//     an actuator is chosen, so naming a different primitive cannot route
//     around them (AC-7).
//   - Scope ladder (CheckScope): a primitive runs only at a scope at or
//     above its declared minimum tier; a non-green selftest refuses outside
//     L0 (AC-19); L5 (real provider) needs the explicit opt-in, an allowlist
//     and a spend cap.
//   - Load gate (LoadGate.Check): refuses to land faults on a saturated
//     host, and the refusal names the measured numbers against the gate
//     (AC-8).
//   - Per-run armed set (ArmedSet): the armed faults live in the run, never
//     in global config; two runs never see each other's faults (AC-12).
//
// Glossary (names owned here or reused from the catalog):
//
//   - Target: the resolved thing a fault lands on (kind + selector).
//   - Refusal: a typed no carrying one reason from the closed taxonomy, the
//     verdict it grades (aborted, PRD §6.4), and the CLI exit code it maps
//     to (2).
//   - Protected target: a control plane (the scheduler, the gateway, the
//     memory daemon) refused by default; an operator may name it explicitly
//     off the automatic path, and the resolution then says so
//     (Target.ProtectedAllowed — the journal records it).
//   - Structural exclusion: PID 1, kernel threads, mischief's own process
//     tree and its reverter — refused unconditionally; there is no override,
//     because these are the "never, by construction" targets (PRD §7).
//
// NOT-LIST (what SPEC-03's rails do not promise here):
//
//   - No blast accounting: the blast bound is a SPEC-03 deliverable this
//     package does not yet carry; nothing here sizes what a fault may break.
//   - The sanction marker (a host admits mischief only with an explicit
//     marker) is SPEC-13 / MSF-020, not here; the host-level refusal lands
//     with that row.
//   - The load gate measures only when asked; it never watches in the
//     background and never writes state.
//   - The armed set is an in-memory record of one run; durability belongs to
//     the journal (SPEC-02) and ownership to the reverter (SPEC-04).
//   - Refusals carry the verdict vocabulary value (aborted) but grade
//     nothing — the verdict engine (SPEC-05) owns grading.
//
// ch:trace row=MSF-004 spec=docs/SPEC-PLAN.md (SPEC-03) + docs/prd/mischief-v0.1.md (§5, §7) evidence=internal/rails/ witness=none:no-live-host-run-in-worktree
package rails
