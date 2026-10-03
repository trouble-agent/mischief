// Package backend_signal is SPEC-06: the process & syscall fault backends —
// signal primitives (SIGSTOP/SIGKILL), prlimit squeezes (NOFILE/NPROC), the
// LD_PRELOAD fault shim (rule language + per-pid counters) and the seccomp
// user-notify supervisor stub.
//
// The one law of this package (docs/prd/mischief-v0.1.md AC-3, the
// anti-gaming criterion): a primitive that does not land grades no_op and
// the run FAILS. Every Apply returns an Outcome whose Status is landed only
// when a measured landed-proof fired — a delivery receipt plus a /proc state
// observation, a prlimit read-back through two independent surfaces, a
// counter file the shim wrote independently of the target. An
// armed-but-unobserved application is no_op with ExitCode 1; a
// green-but-did-nothing backend cannot pass.
//
// Glossary (names owned here, reused by later specs):
//
//   - Outcome: the measured result of one fault application (status,
//     receipt, landed-proof, no-op reason). SPEC-05 grades verdicts from
//     these; the backends never grade a verdict themselves.
//   - Receipt: the measured evidence that the fault mechanism was DELIVERED
//     (kill() returned, prlimit64 returned, env attached) — necessary but
//     never sufficient for landed.
//   - LandedProof: the independent observable proving the fault is ACTIVE
//     (/proc state T, limits read-back, the shim counter file).
//   - Rule: one MCFAULT rule (sys:pattern:errno[:budget]) with anchored
//     path matching and a self-excluded proof channel.
//   - ShimArm: a shim fault attached to a not-yet-started child; Prove is
//     called after the child exits.
//   - UnavailableError: a capability_unavailable refusal naming what is
//     absent (AC-11) — the seccomp supervisor stub returns only this.
//
// Anchoring (the MSF-028 rule-language finding, deliberately NOT copied):
// the legacy probe shim (probe/libfault-probe.c) matched any
// /proc/<pid>/fd target that CONTAINED the pattern — bare substring, no
// anchoring, and the proof counter file itself could match the rule. The
// v0.1 language refuses bare substrings at Parse time; a pattern is exact,
// a path-boundary prefix ("dir:*": the dir itself or anything under it) or
// a path-boundary suffix ("*:name": the name or anything ending in
// "/name"). The proof channel is self-excluded twice: the shim never
// matches a write to the proof path, and Parse refuses a rule whose
// pattern matches its own proof path.
//
// NOT-LIST (what SPEC-06 does not promise):
//
//   - Signals are rootless same-user only: the backends refuse pid <= 1
//     and never elevate. Protected-target policy is SPEC-03's rails, not
//     here.
//   - prlimit64/seccomp(2) syscall numbers are wired for GOARCH=amd64 only
//     (302 / 317, verified live on this host); any other GOARCH grades
//     capability_unavailable naming the architecture — never a guessed
//     number.
//   - The seccomp supervisor is a design stub: it always refuses with
//     capability_unavailable naming the missing helper. It does not
//     supervise; a green-but-does-nothing seccomp backend would be exactly
//     the AC-3 gaming this package exists to prevent.
//   - The shim needs a compiled libfault.so; without one (and without a C
//     compiler to build it on demand) Arm returns a named error instead of
//     silently running the target unfaulted.
//   - No thread-directed signals (tgkill, P-002), no SIGSEGV family
//     (P-009), no real-time storms (P-011): v0.1 implements SIGSTOP,
//     SIGCONT and SIGKILL; anything else is refused by name.
//
// Usage: build a fault, call Apply, read the Outcome; revert through the
// recorded inverse (Resume for signals, Restore for prlimit, detaching the
// rule for the shim — a shim fault is per-process by construction).
// Selftests live here too: each backend has a Selftest* function with the
// land → prove landed → revert → prove reverted contract, and Gate enforces
// the AC-19 refusal (an un-selftested primitive refuses outside L0).
//
// ch:trace row=MSF-007 spec=docs/prd/mischief-v0.1.md (SPEC-06) evidence=internal/backend_signal witness=probe/RESULTS.md
package backend_signal
