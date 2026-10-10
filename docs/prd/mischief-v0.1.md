# mischief — fault injection for a fleet of Linux daemons

**Draft v0.1 PRD · status: ACCEPTED · name: mischief (decided 2026-09-28, MSF-001)**
Repo home: `github.com/trouble-agent/mischief` · org-adjacent to `trouble` · Visibility: private-first
Decided by the owner in MSF-001 (complete 2026-09-28, commit b36acc1). Drafted 2026-09-27 ·
Author: the fleet agent, from live evidence on this host

> This is the front door. The annexes are `docs/FAULT-CATALOG.md` (every fault
> primitive, what it breaks, how it proves it landed, how it is reverted) and
> `docs/SPEC-PLAN.md` (the spec set and the spec→AC map for the build).
> `probe/RESULTS.md` carries the measured capability audit this design rests on.

---

## 1. One-liner

mischief declares a fault, lands it inside a named Linux target (process, cgroup,
container, host or simulated cloud resource), **proves the fault actually landed**,
holds it under a TTL owned by a reverting watchdog that outlives the CLI, and grades
whether the target recovered — so "it recovers" stops being an assumption and becomes
a verdict with a journal behind it.

## 2. The problem — with the fleet's own case files

### 2.1 The fleet's entire fault-injection surface is 5 bash cells, and 4 of them have lied

`~/…/bunker-qa.sh` is the only harness-owned thing that injects faults.
It carries five cells — `chaos-disconnect`, `chaos-shutdown`, `chaos-corruption`,
`chaos-resource`, `chaos-errorpath` — and its own comments record that each has
graded something that was not the product (counted live from the file today):

| case file | what the cell graded | what was actually true |
|---|---|---|
| QA-TROUBLE-3 | `chaos-corruption` — false **green** | the harness ran `go run .` and never started the app (repo's main packages live in `cmd/`); the restart path was never exercised |
| QA-TROUBLE-2 | `chaos-disconnect` verdict | a harness artifact: the 120 s window was shorter than the ~2.5 min suite, later resized to 2×+30 s |
| QA-TROUBLE-6 | `chaos-resource` INFO | the full suite OOM'd inside the harness's own 3 GB cap — a statement about the cap, not the product |
| QA-TROUBLE-8 | `chaos-shutdown` FAIL rc=125 | the JIT agent's static docker CLI was missing; the cell measured the harness |
| QA family (QA-19/20) | `chaos-corruption`/`chaos-errorpath` | vacuous `rc=1` verdicts on a build that never completed |

**5 cells, at least 4 of which have produced a verdict about the harness rather than
the product.** None of them can distinguish *"the fault landed and the product
survived"* from *"the fault never landed"* — which is the single most expensive false
green in this class, and it is the reason the file now carries a `build_incomplete`
predicate and a 40-line comment about false passes instead of a fault library.

### 2.2 Where recovery was unproven, it cost real things

- **TRBL-031 (light-hub)**: after Redis came back, the runtime never re-ran
  `EnsureGroup`, so an empty server on the same address left ingestion dead. The
  recovery code existed and was unreachable; a hand-made `docker stop` probe found it,
  no battery could have.
- **TRBL-040/041 (flow spool)**: `Desk.EnqueueSpool` was *durable in name only* — it
  wrote payloads under a key no replay loop ever lists. A live probe proved it; a
  `kill -9`-between-enqueue-and-drain experiment is the test that would have found it
  by construction.
- **The 2026-08-29/30 archive hole**: backups existed, the restore path had never been
  exercised under a *live* writer, and two days of chat archive are permanently gone.
  The fault that caused it (`state.db` replaced underneath a running process) is a
  production incident the fleet hit and never tested.
- **bunker 0.1.4 `reconciliation.mode=adopt`**: a boot with a missing registry record
  *destroyed* a running agent. A restart is a fault. Nothing in the fleet models it.

### 2.3 There is nothing to ask "does X recover from Y?" and get a number

- **527 lanes** registered on the scheduler (**347 enabled**) — the fleet's own
  registry, read live with `?limit=1000`.
- **30 of 149** scanned repos carry at least one fault-shaped primitive inside their
  own test files (`SIGSTOP`/`SIGKILL`/`netem`/`ENOSPC`/`LD_PRELOAD`): ad-hoc,
  per-repo, unstandardised, no shared catalog, no verdict vocabulary, no landed-proof.
- The only shared harness is the 5-cell battery in §2.1.

The asymmetry is stark: inducing a fault costs seconds on a box the fleet already
owns; the recovery bugs it finds currently escape into production and cost days.

## 3. Who it is for

| role | what changes for them |
|---|---|
| Fleet operator | "does project X recover when its dependency dies / its disk fills / its queue is killed mid-write?" gets a verdict, a journal and a duration — not a guess |
| The incident brain (`trouble`) | it gets an adversary that induces exactly the faults its sensors claim to detect, so its **detection coverage** becomes a measured matrix instead of an assertion |
| A project's foreman (an agent on a tick) | a fault battery is a job it can run and file findings from, like any other test suite — regressions in resilience become rows |
| A project's maintainer / its CI | `mischief run` is a pass/fail command with a machine-readable verdict; the fault catalogue is data, so a project adds a case without writing Go |
| An outside contributor | a reported recovery failure replays from a journal + seed on their own laptop |

## 4. What it does — the loop, and what closes it

```
declare ─▶ plan ─▶ arm ─▶ LAND ─▶ prove landed ─▶ hold ─▶ observe ─▶ revert ─▶ prove reverted ─▶ grade ─▶ journal ─▶ findings
 (yaml)   (no side   (actuator + detached          (probes +                 (inverse,                (7 verdicts)  (jsonl)    (board rows)
           effects)   reverter, TTL)                assertions)               measured)
```

**Closure rules (each loop states what makes it closed):**

1. **Fault loop** — closed when the fault's `landed_proof` reports landing AND its
   `inverse` reports reverted, both measured out-of-band. A fault that was armed and
   reverted but never landed is **`no_op` and the run FAILS** — it is not a pass.
2. **Recovery loop** — closed when the pre-fault baseline probe set is green again
   within the declared budget AND the integrity oracle (checksums/counters/ledger
   sequence) shows no loss. Liveness alone never closes it.
3. **Detection loop** (when `observe.trouble` is declared) — closed when the target's
   incident ledger contains the expected record within the window, or when it is
   recorded as a **detection gap** on the owning project's board. A detection gap is a
   finding, not a run failure.
4. **Adoption loop** — a primitive is closed when it is in the catalog, has a
   published landed-proof, passes `mischief selftest --primitive <id>`, and has a
   battery cell. Until then it is marked `L1-only` and refuses to run outside a
   scratch scope. A primitive with no selftest cannot run on a shared host.

**Two execution modes, one engine:**

- **experiment mode** (`mischief run|battery`) — deterministic, seeded, CI-able, one
  declared fault set, one verdict. This is the mode the fleet consumes.
- **monkey mode** (`mischief monkey`) — random but *seeded and catalog-bounded*:
  a profile picks from the catalog under a concurrency cap, a window and an abort
  file; every pick is logged before it lands, and every pick is revertible. Randomness
  lives in the *selection*, never in the *inverse*.

## 5. The one architectural decision

> **A fault is data with three obligations: an `inverse`, a `landed_proof`, and an
> enumerable `blast` — delivered by an actuator that may be privileged and a reverter
> that must not die with it.**

Alternatives, and why they were rejected:

| shape | why not |
|---|---|
| fault = a shell snippet (what today's 5 cells are) | cannot be inverted reliably, cannot prove it landed, blast = whatever the author remembered |
| fault = ptrace only | cannot express resource, network, storage or cloud faults |
| fault = cgroup only | cannot express syscall, filesystem or network faults |
| fault = a k8s CRD (Chaos Mesh shape) | the fleet is not k8s; a CRD model imports an API server, a controller loop and a CRD schema for 5 hosts' worth of durability |
| **fault = data + pluggable primitive backends behind one contract** | adopted — catalog is data, backend is code, and *every* fault inherits the three obligations instead of re-implementing them |

**Backends (chosen after the capability audit in §9, not before):**
LD_PRELOAD shim (rootless, path-aware, counted), seccomp user-notify supervisor
(rootless, syscall+path aware), `systemd-run --user` transient scope (rootless
resource limits), signal/`prlimit` primitives, privileged helper over a narrow
`sudo` allowlist (netns+netem, dm/loop, cgroup freezer, fsfreeze), docker API
(container nodes), a userspace HTTP/DNS/TLS proxy (network faults with zero
privileges), and a provider **simulator** (cloud shapes without a cloud).

**The wrong turn this closes**: a global "armed faults" file. The armed set is
**per-run session state** — `mischief run` arms exactly the primitives its experiment
declares, and two concurrent runs must not see each other's faults. The catalog is
constant; the armed set is per-run and mutable; execution state (supervisor fds,
shim counters, helper sockets) is per-run and never pooled.

**The other wrong turn**: growing the surface per target. The tool count is fixed by
the *verb* count; the target is an **argument** resolved explicit-argument →
experiment-declared scope → refusal. There is no default target, ever.

## 6. Interface

### 6.1 Verbs (parameters, not verb names)

Status reconciled against v0.1.0 (`cmd/mischief/main.go` dispatch +
`make bin && ./bin/mischief --help`): **shipped** rows are in the current binary;
**planned** rows keep their PRD intent and are not implemented in v0.1. A hidden
`__owner` verb (the detached TTL owner spawn target) exists in the dispatch but is
deliberately not an operator verb and is not listed in `--help`.

| verb | v0.1 | arguments | returns / side effects |
|---|---|---|---|
| `mischief plan -f <exp.yaml> [--json] [--scratch-dir D]` | shipped | experiment file | resolved targets, primitives, landed-proofs, inverse plan, blast bound, capability verdicts, refusals. **No side effects.** exit 0 clean / 2 refused |
| `mischief run -f <exp.yaml> [--seed N] [--ttl 60s] [--dry-run] [--out <journal.jsonl>] [--observe trouble=<ns>]` | **planned — not implemented in v0.1** | experiment + overrides | run id, verdict, journal path, timings. (The reverter hold/TTL/kill-switch machinery ships — see `status`/`revert`/`serve` below; the experiment-driven arming loop does not.) |
| `mischief status [--dir D] [--json]` | shipped | — | boot id, live holds (fault, target, TTL remaining, `stuck_at` leftovers from previous boots), proof history |
| `mischief revert (--all \| --hold <id>) [--dir D]` | shipped | selector | forced revert + the measurement proving reverted. `--all` is the kill switch over every live or stuck hold (AC-6) |
| `mischief catalog [--layer P\|S\|R\|F\|N\|C\|I\|T] [--json]` | **planned — not implemented in v0.1** | filter | per primitive: id, layer, params schema, landed-proof, inverse, capability requirement, safety class, default blast. (The catalog ships as data today; `doctor` prints capability verdicts.) |
| `mischief selftest (--all \| --primitive <id>) [--dir D] [--catalog-dir D]` | shipped | scope | per primitive: land, prove landed, revert, prove reverted on a scratch target. exit 0 only if all proofs pass (green-or-skip — the M1 exit criterion) |
| `mischief monkey --target <selector> --profile <name> --seed N [--max-concurrent 1] [--window 30m] [--max-faults K] [--abort-file <path>]` | **planned — not implemented in v0.1** | target **required** | seeded selection log, verdicts, findings |
| `mischief battery [--quick] [--project P] [--primitive ID] [--out D] [--dir D] [--file-rows] [--catalog-dir D] [--dry-run]` | shipped | the fault × target matrix, derived from the catalog × projects/primitives (there is no matrix-yaml input) | coverage matrix artefact (`matrix.json`/`matrix.md`) + `battery.jsonl` journal; `--file-rows` emits `findings.jsonl` board-vocabulary rows for adverse verdicts — emitted for the operator/foreman merge step, not appended onto a live board; `--dry-run` resolves and prints the matrix without executing |
| `mischief doctor [--catalog-dir D] [--json] [--self-check]` | shipped | — | capability table; unsupported primitives return `capability_unavailable` **naming what is absent**; `--self-check` runs the rail self-checks. exit 1 = a check FAILED (catalog load, or a `--self-check` rail) |
| `mischiefd` | **planned — v0.1 ships `mischief serve --dir D` in its place** | config | the reverter/watchdog daemon; owns TTLs, reconciles leftovers at boot, serves `/health.json` |
| `mischief chaos (matrix --project P [--tier-max N] [--out D] \| check --lane F \| finding --project P --board D --title T --reason R)` | shipped | one subcommand per lane duty | `matrix`: derive the project-scoped fault matrix from the project's own capability data — never executes a fault, never touches a board; `check`: verify one instantiated lane (template contract isolation-first, then board symlink evidence) — refuses a severed lane; `finding`: file ONE finding as a board-vocabulary row on the owning board, validated + duplicate-guarded before the append. Deliberately dumb: derives, verifies, files — never arms a fault or edits the lane |
| `mischief sim --shape I-00N [--path P] [--log F] [--allow-real --resource TAG --spend-cap USD]` | shipped | layer-I catalog id | arm one cloud shape on the local provider simulator, issue ONE real request against it, print the AC-17 landed proof backed by the sim's request log; `--allow-real` + `--resource` + `--spend-cap` apply the AC-17 real-provider gate explicitly (v0.1: the real adapter is a stub — the gate decision is printed and the redirect-to-sim path proves itself; `destroy_class` =permanent refuses outright) |
| `mischief serve --dir D` | shipped | run dir | the reverter daemon for that dir: TTL expiry, leftover reconcile at boot, serves `/health.json` (the v0.1 form of the planned `mischiefd`) |
| `mischief install [--user U] [--prefix P] [--dry-run] [--no-drop-in]` | shipped | target prefix (+ grantee user) | install the chassis artefacts and the ONE sudoers drop-in generated from the canonical verb table. Never elevates: writes only what the caller already has rights for and refuses over a foreign file; a real drop-in write is refused without visudo validation; `--dry-run` prints the exact plan and touches nothing |
| `mischief uninstall [--prefix P] [--runs-dir D] [--keep-count N] [--keep-days D] [--dry-run]` | shipped | target prefix | remove the installed artefacts, optionally expiring run dirs per the retention policy first; dry-runnable |
| `mischief audit [--prefix P] [--json]` | shipped | state prefix to survey | operator diff: drop-in installed + matches the verb table, helper manifest matches, setuid binaries in the prefix (the design ships none — any hit is a finding), per-verb grant table. exit 1 on drift or a setuid finding; `--json` is the same verdict as a surface |
| `mischief retention [--apply] [--runs-dir D] [--keep-count N] [--keep-days D] [--dry-run]` | shipped | run-dir rotation policy | report the rotation plan (default: report-only) or apply it (`--apply`): keep at most N newest run dirs (default 20), expire run dirs older than D days (default 30); `--dry-run` prints the exact plan and touches nothing |
| `mischief plane (parse-agent-id [--in FILE\|-] \| record --file E --step S --status pass\|fail\|skip [--detail TEXT] \| selftest-verdict --exit-code N \| fold --file E)` | shipped | bunker test-plane evidence | bookkeeping-only evidence CLI: extract the agent id from `bunker spawn` output; append one JSONL evidence row; print pass\|fail from the remote selftest's exit code; grade (fold) a JSONL evidence file — exit 0 pass / 1 fail / 3 skip. Never spawns, ssh-es or destroys anything (the driver owns the acts; this owns the bookkeeping) |
| `mischief version` | shipped | — | print the stamped git sha (`unknown` = an unstamped build — never a fabricated sha) |

### 6.2 Experiment (a fault case is data)

```yaml
id: redis-loss-recovery            # display name; the RUN id is content-derived
target:
  selector: container:trouble-hub  # process | cgroup | container | host | cloud
  scratch: false                   # true ⇒ refuse non-scratch blast
faults:
  - primitive: I-002               # infra: dependency restart / cold-return
    params: {service: redis, port: 7699, mode: fresh-empty}
    ttl: 90s
    landed_proof: {kind: external-probe, probe: tcp:7699, expect: refused-then-open}
    inverse: {primitive: I-002, params: {mode: restore}}
probe:
  - name: ingest-accepts
    cmd: "curl -sf -o /dev/null -w '%{http_code}' localhost:7661/health.json"
    expect: "200"
  - name: ledger-contiguous
    cmd: "mischief-oracle ledger-seq <state_root>"
    expect: "contiguous"
budget: {recover_within: 120s, max_error_rate: 0.05}
observe:
  trouble: {namespace: trouble, expect_record: TROUBLE-HUB-004, within: 60s}
integrity: {files: ["<state_root>/ledger/*.jsonl"], mode: hash-and-append-check}
seed: 20260927
```

### 6.3 Journal (append-only JSONL — the evidence, same doctrine as `trouble`'s ledger)

Record types: `run_planned`, `fault_armed`, `fault_landed` (carries the proof),
`probe_sample`, `assertion_result`, `observe_result`, `fault_reverted` (carries the
revert proof), `verdict`, `run_closed`, `finding`. Group-commit + fsync, content-derived
run ids (`hex(sha256(spec+seed))[:16]`), never re-serialised in place.

### 6.4 Verdict vocabulary (closed, and every value is representable end to end)

`recovered` · `degraded` (passes with a measured cost) · `not_recovered` · `hung` ·
`corrupted` (data loss detected) · **`no_op`** (fault never landed) · `aborted`
(rails/TTL/blast refusal) · `flaky` (N runs disagree) · `void` (control run not green).

`no_op`, `aborted`, `corrupted` and `flaky` exist for one reason: they are the shapes
a green-but-meaningless run would otherwise take. A vocabulary without them is how the
5 cells in §2.1 kept passing.

## 7. Safety doctrine — the part that decides whether this is allowed to exist

**Never, by construction:**

- **No default target.** A verb without a resolved target refuses (exit 2).
- **Never its own tree**: mischief's own process tree, its reverter, PID 1 and kernel
  threads are excluded at the resolver, not by convention.
- **No global armed set.** Faults live in a run, not in config.
- **Never a global-config side effect for a per-run choice** (the enabled primitive
  set is run state; the catalog is immutable).

**Scope ladder (which primitive set is reachable at which tier):**

| tier | scope | privileges | what runs |
|---|---|---|---|
| L0 | scratch harness owned by mischief | none | everything, always allowed — where `selftest` lives |
| L1 | a transient `systemd-run --user` scope / a process tree | rootless (**proven live**) | resource limits, signals, LD_PRELOAD, seccomp, HTTP/DNS proxy |
| L2 | host-scoped (netns, dm, loop, freezer, fsfreeze) | via a **narrow `sudo` allowlist** — no setuid, ever | network, storage, host-level faults |
| L3 | container/node lifecycle | docker API | stop/kill/pause/OOM/restart-loop/volume-loss |
| L4 | provider **simulator** | none | cloud shapes (429s, 5xx, eventual consistency, stale reads, token expiry, spot reclaim, volume loss, DNS propagation) |
| L5 | **real** cloud provider | opt-in `--allow-real` + allowlist + spend cap | reversible, non-destructive verbs only; `destroy_class=permanent` **refused in v0.1** |

**Where mischief ITSELF is allowed to run (SPEC-13, MSF-020).** Every other project in
this fleet may build and test on the host the scheduler lives on. This one may not,
because its work *is* the fault. The rule:

> **Mischief's own development, selftest and battery runs execute on an ephemeral
> sanctioned host — a bunker instance — never on the fleet's main host.**

The main host carries the scheduler, the gateway, the memory daemon and the live lanes;
a fault injector pointed at it by accident is not a bug, it is an outage. So the main
host is a **protected target that refuses**, and the refusal is mechanical rather than
documentary:

- A host admits mischief only when it carries an explicit **sanction marker** (file +
  env). Absence is a REFUSAL with a non-zero exit, not a warning — fail closed.
- Protected targets (the scheduler, the gateway, the memory daemon, PID 1, kernel
  threads, mischief's own process tree and its reverter) are evaluated at **target
  resolution**, before an actuator is chosen, so naming a different primitive cannot
  route around them.
- The load gate refuses to land a fault on a saturated host. An injector that adds load
  to a struggling box manufactures the incident it claims to observe.

**The interaction the spec must state, because it constrains the catalogue:** a bunker
agent is unprivileged (measured: uid ≠ 0, no passwordless `sudo`, `unshare -rn` fails at
the `uid_map` write), so the **L2/L3 primitives cannot be exercised there**. The spec
names, per tier, the sanctioned environment — and which primitives are L0/L1-only in
v0.1 because no fleeting sanctioned host can run them. **A primitive that cannot be
proven on a sanctioned host does not ship.**

**Held at all times:**

- **TTL + detached reverter.** The inverse is recorded (write-ahead) *before* the fault
  lands; a separate process owns the TTL and reverts even if the CLI is `kill -9`ed.
  Boot reconcile marks leftover faults `stuck` and surfaces them in `status`/`/health.json`.
- **Kill switch** `mischief revert --all` — proven reverted, not merely issued.
- **Load gate.** Refuses to land faults when the host is already saturated (PSI/loadavg)
  — the same doctrine the fleet applies to itself.
- **Protected targets.** The scheduler, gateway, memory daemon and the fleet's control
  planes are `protected=true` and unreachable from the automatic (monkey) path; an
  operator can name them for an experiment, and the run then says so in the journal.
- **State, not data.** File/storage faults act on declared scratch paths unless the
  experiment names a real path *and* carries a backup assertion. Nothing destructive is
  in scope for v0.1.
- **No secrets in the journal.** The journal is scrubbed on write (credential shapes,
  tokens) — the same contract as `trouble` SPEC-02.

## 8. Division of labour with `trouble`, and the hooks that make this adoptable

| | `trouble` | `mischief` |
|---|---|---|
| one question | *what happened, and what fixes it?* | *what breaks, and does it come back?* |
| altitude | detect → record → research → remediate (plays) | induce → prove → hold → revert → grade |
| blind spot | cannot prove its own detection coverage; its plays are unpriced until a fault arrives | cannot remediate; an injected fault with no observer is just damage |

**Integration points, ordered by process-change cost (all additive until #4):**

1. **`observe.trouble` — read-only assertion.** An experiment may declare "the target's
   ledger must contain record R within N seconds". Costs `trouble` nothing; turns its
   detection claims into a measured matrix (fault × detected × time-to-detect).
   **This is the hook that decides adoption**, because it is the only one that needs
   zero changes to any existing project. *(Implemented v0.1: `internal/observe` — a
   read-only ledger client whose type exposes no mutating method, polling under the
   declared window, naming the matched ledger line in the journal's `observe_result`
   record and filing a detection-gap row on a miss. The ledger wire shape is the
   v0.1 scratch contract (`GET /api/v1/namespaces/<ns>/records?since=…&limit=…` →
   bare JSON array); the live wiring against a real `trouble` instance's
   findings-ledger API is v0.2 — the `Line` decode struct is the only seam that
   changes when it lands.)*
2. **`mischief battery --file-rows`** — findings become rows on the *owning* project's
   board, in that board's own vocabulary. Additive, uses the existing filing path.
3. **`mischief battery --quick` as a QA cell source** — the five bash cells in §2.1 are
   replaced by catalog primitives that carry landed-proofs. This *deletes* code, so it
   is a migration with a parity proof, not a rewrite.
4. **`mischief verify-play`** (later) — after `trouble` records a play that fired,
   re-inject the original fault to check the fix held. Needs `trouble`'s ledger schema
   read-only; the only hook that makes `mischief` part of a closed remediation loop.
   *(v0.1 ships the refusal, not an engine: `observe.VerifyPlay` fails closed under
   every option combination — including `--allow-real` — and names what is absent
   (the live ledger-schema read and the re-injection executor).)*
5. **A `-chaos` satellite lane** (opt-in, per project, its own namespace) that runs the
   project's fault matrix on a cadence and files findings. New convention ⇒ last.

## 9. Platform audit — measured on this host, before the build (2026-09-27)

Required capability → probed live → consequence. `probe/RESULTS.md` carries the raw
output, `probe/` carries the scripts, so every row is re-runnable.

| capability | state | measured evidence | consequence for the design |
|---|---|---|---|
| cgroup v2 unified | **PROVEN** | `fstype=cgroup2fs`; controllers `cpuset cpu io memory hugetlb pids rdma misc dmem` | resource faults are cgroup-shaped |
| rootless resource limits | **PROVEN** | `systemd-run --user --scope -p MemoryMax=64M -p CPUQuota=10% -p TasksMax=32 true` → OK | L1 default: memory/CPU/pids faults with **no privileges** |
| cgroup mkdir at root as uid 1000 | **DENIED** | `cgroup_mkdir=DENIED(unprivileged)`; own slice exposes `cpu memory pids` but is not writable | privileged helper, or transient scopes only |
| cgroup freezer | **PROVEN as root child** (my first probe was wrong) | root cgroup has no `cgroup.freeze`; a child cgroup created under sudo exposes `cgroup.freeze` + limit files (4 matches) | freeze/hang faults need L2; freezer is a core feature, not a v2 controller |
| PSI | **PROVEN** | `/proc/pressure/{cpu,io,memory}` present, `io some avg10=19.35` | pressure faults + the load gate |
| seccomp user-notify | **PROVEN (API)** | `seccomp_user_notify=SUPPORTED notif=80 resp=24` | syscall/path error injection backend |
| LD_PRELOAD fault **with landed-proof** | **PROVEN end-to-end** | control: `write(...) -> 6`; faulted: `write(...) -> -1 errno=Input/output error`, target file 0 B, per-pid counter file written | the flagship rootless primitive AND the proof-of-landing rule |
| netns + netem | **PROVEN with sudo** | `ip netns add` OK; `tc qdisc add dev lo root netem loss 100%` OK (qdisc reported `loss 100% seed 844288704255200251`); `ping` rc=2; `netns del` OK | L2 network faults; revert proven the same minute |
| unprivileged userns network | **PARTIAL** | `unshare -Un true` OK, but `ip link add veth` → `EPERM` (Ubuntu AppArmor userns restriction) | network faults need the helper **or** a `NET_ADMIN` container — this is why docker is a first-class backend |
| time namespaces | **NOT AVAILABLE rootless** | `unshare --time --boottime 1000 true` → `Operation not permitted` | clock faults (T-layer) go through the shim/helper, or stay out of v0.1 |
| docker | **PROVEN** | `docker=29.1.3 cgroup=2` | container nodes; fallback for `NET_ADMIN` |
| loop / device-mapper | **UNPROVEN** (tools present) | `/dev/loop-control`, `/dev/mapper/control`, `losetup -f → /dev/loop27`, `dmsetup` + `mkfs.ext4` present | EIO/slow-disk classes land at M2 behind the L2 helper, each gated by selftest |

**Resized scope, one line:** 6 of 11 required capabilities already ship; 2 need the
narrow privileged helper, 1 needs a container, 1 needs the shim, and 1 (time
namespaces) stays out of the rootless set. Nothing in the build list depends on a
capability that is not already present on this host.

## 10. Build plan

| milestone | content | done when |
|---|---|---|
| **M0** decision | name, repo home, privilege model, public/private (§13) | owner answers; repo + board exist |
| **M1** chassis | experiment schema, catalog loader, journal writer, verdict engine, rails, `plan`/`status`/`revert`/`doctor`, reverter daemon + boot reconcile | `selftest --all` green on L0 for the 3 primitives that need no privileges |
| **M1a** isolation contract | SPEC-13 (MSF-020): the sanction marker, the protected-target refusal at resolution, the load gate — enforced, with a test that fails if the marker check is neutered | running mischief's selftest on an unsanctioned host refuses with a named reason and writes nothing; the same run on a sanctioned host proceeds |
| **M1b** bunker test plane | MSF-021: every tick that exercises a primitive runs on an ephemeral agent — obtain, ship, build (no `make` on a bare agent), run, pull evidence, destroy | one command yields evidence with zero residue on the main host; an unobtainable agent is a recorded SKIP, never a pass |
| **M2** L1 primitives | signals/TTL, `systemd-run` scope limits, LD_PRELOAD shim (counted), seccomp supervisor, HTTP/DNS/TLS proxy | each primitive passes selftest + one battery cell; landed-proof published in the catalog |
| **M3** L2/L3 primitives | netns+netem, dm/loop I/O errors, fsfreeze, RO remount, container lifecycle, volume loss | `mischief doctor` reports each as available with its proof; the sudo allowlist is the only new privilege |
| **M4** trouble loop | `observe.trouble`, detection matrix artifact, `battery --quick` replacing the 5 bash cells (parity proof) | the §12 replays pass; the bash cells are deleted with their replacement named |
| **M5** cloud + monkey | provider simulator, real-provider adapter behind `--allow-real`, seeded monkey mode, soak | a 24 h monkey soak on a scratch host with zero `stuck` faults and zero manual reverts |
| **M6** fleet adoption | `-chaos` satellite lane, per-project fault matrices, public docs | N projects carry a matrix; findings arrive as rows without an operator in the loop |

## 11. Acceptance criteria

Form: `AC-n: Given <state>, when <action>, then <observable result> — proof: <command/test/artifact>. (T|C|M)`
T = automated test, C = command/probe with expected output, M = artifact that must exist.
These transfer verbatim into the board rows' `acceptance_criteria` and the worker briefs.

- **AC-1: Given any experiment, when `mischief plan -f exp.yaml` runs, then no target state changes — proof: plan on a running target leaves `status` empty, the journal file absent and the target's own checksum/pid set byte-identical. (T)**
- **AC-2: Given any verb that lands or holds a fault, when invoked without a resolved target, then it exits 2 and lands nothing — proof: `mischief run -f no-target.yaml; echo $?` → 2, and `mischief status` shows no armed fault. (C)**
- **AC-3 (anti-gaming): Given a primitive whose `landed_proof` never fires (stub backend, wrong target, or a start command that never starts), when the run completes, then the verdict is `no_op` and the run FAILS — proof: inject a deliberately no-op primitive; the journal carries `fault_armed` with no `fault_landed`, and the exit code is non-zero. A green-but-did-nothing implementation cannot pass this. (T)**
- **AC-4: Given any landed fault, when it is reverted, then the revert is MEASURED (the inverse proof), not asserted — proof: `fault_reverted` carries a proof record, and an experiment whose inverse is sabotaged fails the run instead of reporting `recovered`. (T)**
- **AC-5: Given a fault held under a TTL, when the CLI process is `kill -9`ed mid-hold, then the fault is still reverted at TTL expiry — proof: kill the CLI, wait TTL+5s, show the target's pre-fault state restored and the journal's `fault_reverted` written by the reverter's own pid. (T)**
- **AC-6: Given a host with N armed faults (including one left by a previous boot), when `mischief revert --all` runs, then every fault is reverted within 2 s and each revert is proven — proof: command output plus `mischief status` empty; leftover faults from a previous boot are listed `stuck` before the call. (C)**
- **AC-7: Given the default configuration, when an experiment names a protected target (scheduler/gateway/memory daemon) or runs in monkey mode, then the run refuses — proof: `plan` on a protected target exits 2 naming the protection; the same target runs only with the explicit operator flag and the journal records it. (C)**
- **AC-8: Given a host above the configured load/PSI threshold, when a run is attempted, then the fault is refused with `aborted` and the reason names the measurement — proof: force load, run, read the refusal's measured numbers. (C)**
- **AC-9: Given the full verdict vocabulary, when a run ends in each of `no_op`, `aborted`, `corrupted`, `flaky`, `void`, `degraded`, then that value survives end to end (journal → CLI output → findings row) — proof: one test per verdict asserting the journal value and the printed verdict agree. (T)**
- **AC-10: Given the same experiment file and seed, when it is run twice, then the run id is identical and the fault set is identical — proof: two runs, one `diff` of the fault set in the journal, and a third run with a different seed producing a different id. (T)**
- **AC-11: Given a primitive the host cannot support, when it is requested, then the verb stays available and returns `capability_unavailable` naming what is absent — proof: request a dm-error primitive without the helper; assert the error text names the missing capability and that `mischief catalog` still lists the primitive. (T)**
- **AC-12: Given two concurrent runs with different fault sets, when they execute in the same minute, then neither sees the other's faults — proof: two runs, each `status` showing only its own armed set, and the global config file byte-identical before and after. (T)**
- **AC-13: Given a probe design, when a run reports `recovered`, then no part of the verdict rests on the fault's own exit code — proof: a fault backend that always exits 0 without landing grades `no_op`; a target that is alive but not serving fails the probe set and does not grade `recovered`. (T)**
- **AC-14: Given a scratch `trouble` instance, when `I-002` (dependency loss) is injected with `observe.trouble` declared, then the run reports whether the expected ledger record appeared within the window — proof: the journal's `observe_result` plus the ledger line it matched; a missing record is filed as a detection gap row. (C)**
- **AC-15: Given a fault class that can destroy data, when the experiment declares no integrity oracle, then the run refuses to grade `recovered` — proof: run a file-truncation experiment without `integrity:`; verdict is `degraded`/`not_recovered`, never `recovered`. (T)**
- **AC-16: Given any experiment, when it is run with faults disabled (`--no-fault`), then the control run must be green — proof: a control run on the pre-fault tree; a red control makes the experiment `void` and the harness, not the target, is the finding. (T)**
- **AC-17: Given the cloud plane, when a real-provider fault is requested without `--allow-real` and a resource tag, then it refuses and runs against the simulator instead — proof: command output naming the refusal, plus the simulator's own request log showing the fault landed there. (C)**
- **AC-18: Given `mischief battery --file-rows`, when a battery completes, then each finding lands as a row on the OWNING project's board with the journal path in its reasoning — proof: the board diff, plus `boardctl validate` clean. (C)**
- **AC-19: Given a primitive, when it has no green `selftest` on this host, then it refuses to run outside an L0 scratch scope — proof: request an un-selftested primitive against a shared target; assert exit 2 with the reason naming the missing selftest. (C)**
- **AC-20: Given a journal, when it is written, then it contains no credential-shaped material — proof: the scrub test suite (token/key/bearer/DSN shapes) over generated journals, plus a scan of a real run's journal. (T)**

## 12. Success criteria — replayable against real history

- **S-1 (kills the §2.1 false green).** Replay QA-TROUBLE-3's shape: a `cmd/`-layout
  repo whose "start" command never starts the app. The old cell graded OK; `mischief`
  must grade **`no_op`** with no landed-proof in the journal. *Proof:* the journal of
  that run, plus the same experiment against a fixed start command grading
  `recovered`/`not_recovered` instead of `no_op`.
- **S-2 (TRBL-031).** `I-002 redis-loss` with `mode: cold-return` — grade
  `not_recovered` on the pre-fix commit, `recovered` on current `main`, with the rewire
  (fresh empty server, same port) visible in the journal. *Proof:* two journals.
- **S-3 (the 08-29/30 hole).** `F-009 file-replaced-under-live-writer` against a live
  session reproduces the in-flight death → `corrupted`; with the driver's fallback the
  same experiment grades `recovered`. *Proof:* journal + the session's own error text.
- **S-4 (TRBL-041).** `P-006 kill-between-enqueue-and-drain` on the pre-fix spool →
  `not_recovered` with an orphaned spool entry; post-fix `recovered`. *Proof:* two
  journals + the spool directory listing.
- **S-5 (fleet coverage).** One `battery` over N projects emits a
  fault × detection × recovery matrix, and the 5 bash cells in §2.1 are gone, replaced
  by catalog primitives that carry landed-proofs. *Proof:* the matrix artifact + the
  diff that deletes the cells.

## 13. Out of scope / boundaries

- Not a load generator (that is `k6`/`locust`-shaped work; mischief may *accompany* one).
- Not an exploit kit or a pentest tool: no privilege escalation, no payloads, no
  scanning of hosts it was not pointed at.
- Not a k8s operator / CRD set. The fleet is plain Linux boxes and containers.
- Not a Windows/macOS tool. Time-namespace faults are out of the rootless set.
- Not an owner of remediation — that is `trouble`'s play rung; mischief may *verify* a
  play, never perform one.
- Never a permanently-destructive fault in v0.1 (`destroy_class=permanent` is
  simulator-only), and never the fleet's control planes by default.
- Not a SaaS, and no telemetry leaves the box.

## 14. Open decisions (owner's — each with a recommendation)

1. **DECIDED (2026-09-28, MSF-001 actioned): name = `mischief`, repo home =
   `github.com/trouble-agent/mischief`.** ~~Recommend `mischief` in
   `github.com/trouble-agent` (a partner for `trouble`; all candidate names are
   free in the org as of today). Alternates: `accomplice`, `monkeywrench`,
   `poltergeist`.~~
2. **Private first, or public from day one.** Recommend **private first**: the board and
   the replay journals will name fleet internals (project names, incident text), and
   flipping to public later is one command. If public, the repo needs the sibling
   contract first (MIT, `SECURITY.md`, `CONTRIBUTING.md`, no absolute paths anywhere).
3. **Privilege model.** Recommend **rootless-first with a narrow `sudo` allowlist** for
   the L2 set; no setuid binary, ever. The alternative — a `NET_ADMIN` container as the
   mandatory substrate — costs the ability to fault a *plain process on the host*, which
   is where 2 of the 4 case files live.
4. **Cloud plane timing.** Recommend the simulator in v0.1 and the real-provider adapter
   behind `--allow-real` in v0.2, pointed only at a scratch project — never at the
   fleet's own hosts.
5. **Lane now or later.** Recommend: repo + board + this PRD now; arm the base lane and
   a `-chaos` satellite only after this draft is reviewed, so the first tick implements
   rather than re-derives.

**The one fork worth naming:** if the C shim is unacceptable (single-binary purity),
syscall-level faults move to the seccomp-notify backend alone. That keeps the file-level
and syscall faults and loses the cheap path-aware write interception and the shim's
per-pid counters — the landed-proof then has to come from the supervisor's own
notification counter, which works but is a heavier runtime (one supervisor per target).

## 15. Why anyone outside this fleet wants it

The upstream field is k8s-shaped (Chaos Mesh, Litmus, AWS Chaos Monkey) or
load/network-shaped (toxiproxy, `tc`, `strace --inject`). The tier this tool owns —
**rootless, host-level, path-aware faults against plain Linux daemons, with a
mandatory landed-proof and a graded recovery verdict** — is the tier where the
false greens live: every chaos tool can tell you it ran, and almost none can tell you
*the fault arrived*. Copy-targets are pinned to a tier, not adopted wholesale:
Gremlin's failure taxonomy (taxonomy only — resource/network/state tiers), toxiproxy's
userspace-proxy trick (network faults with zero privileges), `strace --inject`'s
existence proof that syscall error injection is a real primitive, and Pumba/chaosblade's
container-fault list. The differentiator is not the fault list; it is the two rules
nobody else enforces — *prove it landed* and *prove it reverted*.
