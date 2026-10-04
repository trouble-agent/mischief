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
| SPEC-13 | isolation contract | host sanction marker (file + env, reason line, fail closed), unsanctioned-host refusal (exit 2, writes nothing), protected list still evaluated at resolution, the L0–L5 ladder, load gate, per-tier sanctioned environments, L0/L1-only primitives in v0.1 |

## Spec→PRD mapping (nothing in the PRD may be unowned)

| PRD section | spec(s) | acceptance criteria |
|---|---|---|
| §5 one architectural decision | SPEC-01, SPEC-02, SPEC-04 | AC-1..AC-5, AC-10, AC-19 |
| §6 interface & verdicts | SPEC-02, SPEC-05 | AC-9, AC-13, AC-15, AC-16, AC-20 |
| §7 safety doctrine | SPEC-03, SPEC-13 (isolation), SPEC-04, SPEC-07 (allowlist), SPEC-11 (audit) | AC-2, AC-6, AC-7, AC-8, AC-12, AC-17, M1a |
| §8 integration with trouble | SPEC-10, SPEC-12 | AC-14, AC-18 |
| §9 platform audit | SPEC-06..SPEC-09 (each capability row is a precondition) | AC-11, AC-19 |
| §12 success criteria | SPEC-10 (the replays are battery cases) | S-1..S-5 |

## SPEC-13 — isolation contract ( mischief runs on sanctioned hosts only )

**Purpose and the question it answers.** Mischief's own work *is* the fault, so the
host it runs on is part of the safety design. SPEC-13 answers: *may this process run
here at all?* — before any target is resolved, before the scope ladder, before the
load gate. It is the FIRST rail. The contract: mischief's development, selftest and
battery runs execute on an **ephemeral sanctioned host** (a bunker instance), never on
the fleet's main host (PRD §7, "Where mischief ITSELF is allowed to run"; M1a).

**State model.** A host is either *sanctioned* or *unsanctioned*, and the edge is
mechanical:

```
unsanctioned ──(marker: reason-bearing file and/or MISCHIEF_SANCTION=1)──▶ sanctioned
sanctioned  ──(marker removed / host destroyed)──────────────────────────▶ unsanctioned
```

There is no third state and no grace period: absence is a REFUSAL, not a warning —
fail closed. There is no downgrade path in which a refusal becomes a logged warning.

**The sanction marker (the mechanism).**

| half | name | admits when | refuses when |
|---|---|---|---|
| file | `/etc/mischief-sanction` (override: `MISCHIEF_SANCTION_FILE`) | file is readable AND carries a reason line (first non-empty, non-`#` line) | absent, unreadable, empty, whitespace-only, or comment-only |
| env | `MISCHIEF_SANCTION` | set to exactly `1` | unset, or any other value (`true`, `yes`, `0`, empty — no truthiness zoo) |
| admission | either half | env set **or** file-with-reason | neither |

The marker file **must carry a reason line**: the human sentence saying why this host
is sanctioned (who sanctioned it, for what wave). An empty marker is a refusal with
its own wording ("write the reason the host is sanctioned into the marker"), because
an empty yes is how a checkbox replaces a decision.

**The refusal (host admission).** An unsanctioned host refuses with the shared rails
contract: a `rails.Refusal` carrying the taxonomy member `host_not_sanctioned`, the
verdict `aborted` (PRD §6.4), and CLI **exit 2** via `rails.MapExit`. The refusal
text names the hostname and BOTH marker routes — the missing file path with its env
override name, and the env variable with its sanctioning value — so the operator can
see exactly which act is missing on which host. The check **writes nothing**: no
journal entry, no state file, no marker of its own; its only filesystem contact is
one read of the marker path (pinned by test).

**Delivery:** `internal/sanction` — `sanction.Check()` (the first rail), `Inspect`
(the evidence: env state, probed marker path, found, reason line), `Options` (the
injected seams: env lookup, hostname, marker read — tests never touch the real
host). The taxonomy member lives in `internal/rails` (`ReasonSanction`) so the
refusal vocabulary stays closed in one place; a sanction refusal satisfies the same
shape and exit mapping as every other rail refusal.

**Protected targets still apply — the separation of concerns.** Host admission
(sanction) and target protection (SPEC-03) are different rails answering different
questions, and neither substitutes for the other:

| question | rail | evaluated |
|---|---|---|
| may mischief run on this host at all? | `sanction.Check` (SPEC-13) | first, before anything resolves |
| may this fault land on that target? | `rails.Resolve` protected list + structural exclusions (SPEC-03) | at target resolution, before an actuator is chosen |

A sanctioned host does **not** weaken the protected list: even on a freshly
sanctioned bunker, the scheduler, the gateway and the memory daemon still refuse at
resolution, PID 1 / kernel threads / mischief's own tree still refuse unconditionally,
and the operator-override + journal-recording contract (AC-7) is unchanged. SPEC-13
adds a reason to the taxonomy; it removes no rail.

**The scope ladder (SPEC-03's L0–L5, restated here for the isolation contract).** The
ladder bounds WHAT a fault may touch; the sanction bounds WHERE mischief may run; the
load gate (AC-8) bounds WHETHER a saturated host takes more. The ladder's refusal
taxonomy: below the declared minimum tier → `scope_below_minimum`; non-green selftest
above L0 → `selftest_not_green` (AC-19); L5 without opt-in + allowlist + spend cap →
`l5_not_allowed`.

**Per-tier sanctioned environments.** A primitive is provable only where its
sanctioned environment exists; a primitive that cannot be proven on a sanctioned host
does not ship (PRD §7).

| tier | sanctioned environment in v0.1 | carries the sanction marker |
|---|---|---|
| L0 scratch | mischief's own scratch harness — unconditionally sanctioned (selftest lives here) | no (mischief-owned by construction) |
| L1 rootless | the ephemeral sanctioned host: a bunker agent (uid ≠ 0, no passwordless sudo) | yes — this is the marker's production home |
| L2/L3 host & node | a sanctioned **non-fleet** box or container with NET_ADMIN (netns, dm/loop, freezer, fsfreeze, docker API) | yes |
| L4 simulator | the simulator process on a sanctioned (L1-shaped) host; no real provider contact | yes |
| L5 real cloud | opt-in only: `--allow-real` + allowlist + spend cap, on an operator-sanctioned environment | yes |

**L0/L1-only primitives in v0.1.** The bunker is unprivileged — measured:
`unshare -rn` fails at the `uid_map` write, no passwordless `sudo` — so the
unprivileged bunker **cannot exercise the L2/L3 primitives** (netns+netem, dm/loop,
fsfreeze, RO remount, docker-API node faults). Those primitives are therefore
*L0/L1-only in v0.1 deployment*: their live proofs ran once, by hand, on the box that
had the capability, and they ship behind their green selftests until a fleeting
sanctioned host with NET_ADMIN exists in the fleet. Concretely from the current
catalogue: N-001 (netns partition, L2) and C-009 (docker rolling replace, L3) have no
sanctioned environment in v0.1; the L1 primitives (P-001 signals, R-001 scopes,
S-001/S-004 shim faults, T-001 clock) are the workhorse, with N-012 (userspace
proxy, L0) fully unconditionally sanctioned.

**Wiring.** The CLI's run path calls `sanction.Check()` before anything else — before
target resolution, before the catalog is even loaded (a refused host does no work).
The check is pure: env + one file read + hostname. Nothing in `internal/sanction`
imports backends, journal or catalog.

**Edge cases (named).**

- *Marker file missing but env set* → admitted by the env half; the refusal arm
  never reads the file twice.
- *Marker file present but empty, env set* → admitted by the env half (the env is
  the explicit act); the empty file alone would refuse.
- *Hostname lookup fails* → the refusal still fires and names the host
  `<unknown>` — a broken hostname lookup must not become an admission.
- *Marker path override set to an empty string* → the file half simply never
  probes; only the env half can admit. Not an error, not a panic.
- *`MISCHIEF_SANCTION=true`* → refused, and the refusal names the value: one
  explicit sanctioning value (`1`), no truthiness zoo.

**Test file + observable threshold per criterion** (all in
`internal/sanction/sanction_test.go`, table-driven, injected seams — no test touches
the real marker paths or the process environment):

| criterion | test |
|---|---|
| admission matrix (absent/unreadable/empty/whitespace/comment-only refuse; file-with-reason and env-only admit; `true`/`0`/empty env refuse) | `TestSanctionRefusalTable` |
| refusal names hostname + file path + env override + env name + value | `TestRefusalTextNamesHostAndMissingMarker` |
| empty marker refusal names the reason-line requirement | `TestRefusalNamesEmptyMarkerReason` |
| THIS host (no marker) refuses with the real environment — the acceptance run | `TestThisHostRefuses` |
| refused host writes nothing (exactly one marker read, no other fs contact) | `TestSanctionCheckWritesNothing` |
| first-rail order + sanctioned host still refuses the protected scheduler | `TestSanctionFirstRailOrder` |
| env injection cannot leak past the test | `TestEnvVarMatrixIsolation` |

**NOT-LIST (what SPEC-13 does not promise).**

- No host identity: the package does not detect "this is the main host" (no cloud
  metadata, no fingerprinting). The marker is the only input; a main host carrying a
  hand-made marker is operator error — the same territory as the protected-list
  override: recorded acts, not prevented ones.
- No marker expiry/TTL: the ephemeral host's lifetime is the sanction's lifetime;
  revocation is destroying the host or deleting the marker, not a protocol this
  package runs.
- No per-tier enforcement in code: which tier runs where is doctrine (the table
  above); `CheckScope` enforces tier minimums and selftests, not geography.
- No fleet-wide revocation broadcast: nothing here tells other hosts their marker
  is stale.

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
