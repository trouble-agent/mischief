# mischief — spec plan (annex to the v0.1 PRD)

The PRD is the front door; this is the build's spec set. Each spec is written to
**falsify** the PRD (report holes rather than smooth them over) and each carries a
`NOT-LIST`: what the mechanism explicitly does not promise.

| spec | title | owns |
|---|---|---|
| SPEC-01 | fault model & catalog schema | primitive descriptor (params schema, landed-proof, inverse, capability, tier, maturity), catalog loader, catalog versioning |
| SPEC-02 | experiment & journal | experiment schema, run id derivation (`hex(sha256(spec+seed))[:16]`), journal record types, group-commit + fsync, scrub-on-write, retention |
| SPEC-03 | rails & blast | target resolution (no default target), protected list, scope ladder L0-L5, load gate, blast accounting, refusal taxonomy |
| SPEC-04 | reverter & lifecycle | write-ahead inverse recording, TTL ownership, detached reverter, boot reconcile (`stuck`), kill switch, `/health.json` |
| SPEC-05 | verdict engine | verdict vocabulary, probe/assertion model, integrity oracle, control run, `flaky` (N runs), timing model (t_landed, t_symptom, t_detect, t_recover) |
| SPEC-06 | backends: process & syscall | signal primitives, `prlimit`, LD_PRELOAD shim (rule language, per-pid counters), seccomp user-notify supervisor |
| SPEC-07 | backends: resource & storage | `systemd-run --user` scopes, cgroup freezer, dm/loop, fsfreeze, RO remount, scratch filesystems |
| SPEC-08 | backends: network | userspace HTTP/DNS/TLS proxy (rootless), netns+netem via the privileged helper, sudo allowlist contract |
| SPEC-09 | backends: nodes & cloud | docker node faults, provider simulator (API/DNS/object/metadata), real-provider adapter (v0.2, allowlist + spend cap) |
| SPEC-10 | battery & filing | matrix definition, coverage/detection artifact, findings to board rows (per-board vocabulary), `--quick` parity with the legacy bash cells |
| SPEC-11 | operations | install, privileged helper + sudoers drop-in, privilege audit, upgrade/downgrade, `doctor`, self-watchdog, retention/rotation, uninstall |
| SPEC-12 | integration | `observe.trouble` (ledger read-only), detection-gap rows, `verify-play` (v0.2), lane cadence contract, deliver routing |

## Spec→PRD mapping (nothing in the PRD may be unowned)

| PRD section | spec(s) | acceptance criteria |
|---|---|---|
| §5 one architectural decision | SPEC-01, SPEC-02, SPEC-04 | AC-1..AC-5, AC-10, AC-19 |
| §6 interface & verdicts | SPEC-02, SPEC-05 | AC-9, AC-13, AC-15, AC-16, AC-20 |
| §7 safety doctrine | SPEC-03, SPEC-04, SPEC-07 (allowlist), SPEC-11 (audit) | AC-2, AC-6, AC-7, AC-8, AC-12, AC-17 |
| §8 integration with trouble | SPEC-10, SPEC-12 | AC-14, AC-18 |
| §9 platform audit | SPEC-06..SPEC-09 (each capability row is a precondition) | AC-11, AC-19 |
| §12 success criteria | SPEC-10 (the replays are battery cases) | S-1..S-5 |

## Per-spec section skeleton (the shape each spec must fill)

```
1. Purpose and the question it answers
2. State model (the states and, above all, the EDGES: armed-but-not-landed,
   reverted-but-target-still-broken, land-after-revert, stuck)
3. Deliverables (the exact files/records this spec owns)
4. Wiring (who builds it, who calls it, what is nil-safe)
5. No vocabulary of its own (which existing enum/type it reuses)
6. Edge cases (one named case each, with the fallback behaviour)
7. Test file + observable threshold per criterion
8. NOT-LIST (what this spec does not promise)
```

## Build-order rule

A primitive's spec section is written **before** its code, and it must state the three
things the code will be verified against: the landed-proof, the inverse, and the
capability it requires. A primitive without those three is not specced and does not get
built — that is the whole point of the fault contract.

## Non-guarantees (the NOT-LIST every spec must carry)

- mischief does not promise a target *can* recover; it promises to measure whether it did.
- mischief does not promise every fault is revertible — it promises every fault's inverse
  is *recorded before landing*, and that a fault whose inverse cannot be expressed is
  refused rather than attempted.
- mischief does not promise determinism of the target, only of the *fault*: same spec plus
  same seed means the same fault set in the same order.
- mischief does not protect against operator error: `--allow-real`, the protected-list
  override and the L5 adapter are deliberate human acts, and each is recorded in the journal.
- mischief does not replace a project's own tests; it is the adversary those tests do not have.
