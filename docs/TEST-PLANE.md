# TEST-PLANE — mischief's bunker test plane (MSF-021)

One command runs mischief's own development selftest on a **fresh, ephemeral,
sanctioned L1 host** and brings the evidence home:

```sh
scripts/bunker-plane.sh run
```

The plane exists because mischief's selftest must be proven on a host that is
not the developer's workstation: an L1 bunker agent (uid ≠ 0, no passwordless
sudo) is the **sanction marker's production home** (SPEC-13,
`docs/SPEC-PLAN.md`), and a green selftest there is the honest version of
"the L0/L1 corpus works on a bare machine" — not on a host accreted with the
developer's toolchains, daemons and docker socket.

Contract companion: `docs/SPEC-PLAN.md` (SPEC-13 isolation contract,
L0–L5 ladder) and `docs/prd/mischief-v0.1.md` §7. The plane respects the
MSF-020 sanction marker contract: it never bypasses `internal/sanction`; it
**provisions** the marker the contract demands.

## 1. Substrate decision (and why)

| candidate | what it is | verdict |
|---|---|---|
| **bunker-qa.sh-style one-shot JIT** (chosen) | spawn a temp agent, run a bounded battery, pull evidence, destroy | **the plane is this shape** — small script, proven contracts (orphan-safe spawn, base64 remote script, destroy-verify, per-run evidence path) reused, none of its cells imported |
| bunker-soak.sh | boot once, sample for days, fault on an interval | wrong axis: the selftest payload is **bounded-fast** (seconds — measured; no long-lived service, no duration axis to soak). A soak would hold an agent for days to run a 30-second battery. |
| gauntlet (`~/gauntlet`) | fault-injection battery over YOUR already-running app (start-script + health URL) | wrong layer: gauntlet *consumes* a booted app and injects faults; it has **no ephemeral-host concept at all**. mischief's selftest IS the fault injector here — nesting one fault injector inside another grades nothing new. |

The one-shot shape is executed as the **launch/collect-safe single script**
with per-stage evidence rows: every act appends a WHOLE JSONL row through the
tested Go verb before the next act starts, so a driver killed mid-flight
leaves a readable prefix, never the zero-byte evidence file that silently
reads as "no cells" (the systemic failure class this fleet already measured
in its QA harness: 143 of 751 evidence files at 0 bytes).

## 2. The pipeline (stages → evidence rows)

Driver: `scripts/bunker-plane.sh`. Evidence logic: `internal/plane` (unit
tested) exposed as the `mischief plane` verb (`cmd/mischief/plane.go`); the
script **never** re-implements parsing, folding or row encoding in shell.

| stage | act | evidence row (`step`) | on failure |
|---|---|---|---|
| build | host builds `CGO_ENABLED=0` `linux/amd64` mischief stamped with HEAD sha | `build` | FAIL (exit 1) — the plane cannot record without it |
| preflight | `bunker status --server S` must answer **ONLINE** | `preflight` | named **SKIP** (exit 3): server name not in config, unreachable, invalid token — no spawn attempted |
| spawn | `bunker spawn` (≤2 attempts, per-run TTL) + `bunker exec` reachability gate BEFORE hand-off; a spawn that fails the exec gate is destroyed before retry (orphan-safe). Raw ssh is NOT the gate — this server class routes commands through the daemon | `spawn` | named **SKIP** (exit 3): capacity exhausted, transport error, gate-unreachable after retries — allowlisted refusal keywords only (the spawn's connection bundle carries an API key and is never quoted verbatim) |
| ship | repo as one `git archive HEAD` tar.gz + the prebuilt binary via `bunker cp` (the CLI's per-file SCP cannot stream a directory at this size — measured: 191 files as a tree times out, as one 436 KB tar lands in ~8s), each verified on the agent (go.mod present; binary sha256 match — a mismatched binary is never executed) | `ship` | FAIL (exit 1) — the failing leg is NAMED in the evidence row |
| selftest | the selftest script (shipped verbatim via `bunker cp` — see the transport facts in `scripts/bunker-plane.sh`): writes the reason-bearing sanction marker (`$HOME/mischief-sanction`, `MISCHIEF_SANCTION_FILE`), then `mischief selftest --all --dir ~/plane-run` under a timeout | `selftest` | FAIL (exit 1) — graded by the tested exit mapping: **exit 0 = pass (every primitive green-or-skip); anything else = fail** |
| collect | journal `selftest.jsonl` + selftest stdout/stderr pulled back base64 (binary-safe, exec-cap bounded); sha256 + line count + landed-proof counters (`fault_landed` / `fault_reverted` / `outcome:skipped`) cross-checked remote-vs-local | `collect` | FAIL (exit 1) — destination coverage is VERIFIED, not assumed: a pull that lost bytes is a fail, never a green |
| destroy | ≤3 destroy attempts, then a `bunker list` re-check (the list is the honest verdict, not the destroy rc) | `destroy` | `TEARDOWN-GONE` = pass (zero residue); late-landing destroy = `TEARDOWN-RECOVERED` pass; still listed = **`TEARDOWN-LEAK` FAIL** naming the id, server, TTL reap hint and the manual command |
| fold | `mischief plane fold` grades the whole file | — | prints `PLANE <status> at <stage>: <reason>`; **exit 0 = PASS · 1 = FAIL · 3 = named SKIP** |

Artifacts land beside the evidence file: `<evidence>.journal`,
`<evidence>.selftest-stdout`, `<evidence>.selftest-stderr`.

The last evidence row is the run verdict. A driver killed between stages
leaves the last completed row as the verdict and the EXIT trap destroys a
still-live agent (and records that destroy, pass or fail).

## 3. What runs on the agent (the payload)

- the repo at frozen HEAD (`git archive`) — no git history, no credentials;
  bunker agents carry no clone auth;
- the prebuilt `CGO_ENABLED=0` static binary — **a bare agent needs no C
  toolchain and no make; the binary is the build** (verified by sha256
  before execution);
- the SPEC-13 sanction marker: a reason-bearing file at
  `$HOME/mischief-sanction` (`/etc/` is not writable at L1), surfaced via
  `MISCHIEF_SANCTION_FILE` — the plane provisions the marker, it does not
  bypass the check; on an unsanctioned host `selftest` refuses (exit 2)
  and the plane grades that a FAIL. (True since MSF-032: the gate
  previously covered `plan` only, so the verb ran unsanctioned — measured
  2026-10-06 on the scheduler host, filed as MSF-032.)
- `mischief selftest --all`: L0 scratch land+prove+revert per primitive
  (AC-3 landed-proof measured, AC-4 revert byte-compared), exit 0 only when
  every primitive is green-or-skip, plus the AC-19 L1 admission report on
  stdout (which primitives are admitted at L1 on this host).

What comes home: the selftest journal (`internal/journal` record format —
`fault_landed` / `fault_reverted` records with measured proofs), the AC-19
report, and the verified landed-proof counters. These are the same record
types any mischief run journal uses; a reader of a run journal reads a
selftest journal unchanged.

## 4. The skip contract (AC 3/4)

An agent that cannot be obtained is a **named SKIP, never a pass**:

- exit code 3 (distinct from FAIL=1 and PASS=0; 2 = usage);
- the reason NAMES the blocker and the stage: `bunker server X not
  reachable/ONLINE (status: …)`, `bunker agent not obtained on X after N
  attempts (spawn output tail: …)`;
- no selftest ran, so no primitive is reported green by this run — a skip
  grades nothing;
- inside the evidence file the row is `{"status":"skip"}` with the reason in
  the detail; `mischief plane fold` renders a reasonless skip as a **plane
  defect** ("skip recorded without a named reason") rather than letting
  silence read as a clean skip.

## 5. What this plane CANNOT exercise (the L2/L3 set)

The bunker is unprivileged — measured (SPEC-13): `unshare -rn` fails at the
`uid_map` write and there is no passwordless `sudo`. The plane's payload is
therefore the **L0/L1 corpus only**. These primitive classes are structurally
out of reach on the agent; each SKIPs with its named
`capability_unavailable` reason inside the selftest (measured on the first
live run, 2026-10-05 — the reasons below are the agent's own words):

| primitive class | catalog rows | tier | why the agent cannot exercise it (measured) | sanctioned environment it requires |
|---|---|---|---|---|
| netns + netem network faults (partition, latency, loss) | N-001 | L2 | tier L2 with no L0/L1 scratch actuator — the namespace work needs `CAP_NET_ADMIN` the agent must never have | a sanctioned **non-fleet** box or container with `NET_ADMIN` (SPEC-13's L2/L3 row) |
| dm/loop + fsfreeze + RO-remount storage faults | F-layer storage rows | L2/L3 | needs root (or `CAP_SYS_ADMIN`) with device-mapper / loop devices — the agent is uid ≠ 0 by contract | a sanctioned non-fleet box with `SYS_ADMIN` |
| docker-API node faults / rolling replace | C-009, `docker:*` selftest rows | L3 | refused at **tier L3** (AC-19: REFUSED at L1, `selftest_not_green`) even though the agent carries a per-agent docker socket — the socket serves the isolation contract, not L3 fault experiments; the `docker:*` rows additionally skip on no local image | a sanctioned box where docker is the L3 experiment substrate, plus its images |
| freezer / cgroup-filesystem resource faults | R-layer freezer rows | L2 | writable cgroupfs with the freezer controller needs root; the L1-admissible scope shape (R-001 via `systemd-run`) DOES run on the agent where a user manager exists | a sanctioned box with cgroupfs write access |

Granting any of these means moving to the L2/L3 sanctioned environment in
`docs/SPEC-PLAN.md` — a non-fleet box or container with the capability above
carrying its own sanction marker — NOT loosening the bunker's isolation.

## 6. Usage

```sh
scripts/bunker-plane.sh run                  # the whole pipeline
scripts/bunker-plane.sh help
scripts/bunker-plane.sh run --server bunker-mvp --ttl 2h \
    --evidence /tmp/run42.jsonl --repo /path/to/mischief
```

Environment (all optional): `BUNKER_PLANE_SERVER` (default `bunker-mvp`),
`BUNKER_PLANE_TTL` (2h), `BUNKER_PLANE_SSH_PORT` (2223),
`BUNKER_PLANE_HOST_KEY` (`~/.ssh/id_ed25519_bunker_mvp`),
`BUNKER_PLANE_EVIDENCE` (per-run `/tmp/mischief-plane-evidence-<ts>-$$.jsonl`
— never a shared path), `BUNKER_PLANE_REPO`, `BUNKER_PLANE_KEEP=1` (skip the
destroy for debugging; recorded as a skip row naming the destroy command),
`BUNKER_PLANE_SELFTEST_TIMEOUT` (300s).

Exit codes: **0** PASS (agent obtained, selftest exit 0, evidence pulled and
verified, agent destroyed) · **1** FAIL (any stage; the `PLANE fail …` line
names the stage and reason) · **3** named SKIP (no agent; nothing ran) ·
**2** usage.

Local evidence verbs (no bunker needed):

```sh
mischief plane parse-agent-id --in spawn-output.txt
mischief plane record --file ev.jsonl --step ship --status pass --detail "SYNC-OK"
mischief plane selftest-verdict --exit-code 0
mischief plane fold --file ev.jsonl
```

## 7. Local development

`go test ./internal/plane/ ./cmd/mischief/` covers the evidence contract:
agent-id parsing (this daemon's "Agent created:" line, older daemons'
`keys/` token, capacity refusal → "", sub-threshold hex tokens refused),
the exit-code verdict mapping, the fold rules (empty evidence = named fail;
last row wins; reasonless skip = plane defect), JSONL round-trips, and
corrupt-line counting. `bash -n scripts/bunker-plane.sh` syntax-checks the
driver and its generated remote script. The transport facts the driver
encodes (exec re-joining, `--script` escaping, cp verbatim-ness) are
measured live and recorded as comments there — they are daemon-version
specific and re-verify on any daemon upgrade.

## 8. First live run

2026-10-05, server `bunker-mvp` (HEAD `24da8f99fa28`): **PASS** — agent
obtained (exec gate), repo + sha-verified binary shipped, `mischief
selftest --all` exit 0 on the agent: **13 pass / 8 named skip / 0 fail (of
21)**, journal 37 lines pulled sha-verified with landed-proof counters
verified (landed=13 reverted=13 skipped=8), AC-19 L1 admission report on
stdout (F-009, I-00x, N-012, P-001, R-001, S-001 admitted; C-009, N-001,
S-004, T-001 refused at L1), agent destroyed and absent from the server
list (`TEARDOWN-GONE`, zero residue). Evidence:
`/tmp/mischief-plane-evidence-20261005T191221Z-3033614.jsonl` (+`.journal`,
`.selftest-stdout`, `.selftest-stderr`) on the mischief host — ephemeral by
design; the counts in this section are the durable record.

The path to that pass also proved the plane's failure honesty: six earlier
runs graded FAIL or named-SKIP with per-leg reasons (an exec-gate SKIP on a
wrong reachability assumption; two FAILs while `bunker deploy` could not
stream a directory tree; a FAIL that exposed the daemon's `--script`
shell-escaping; a FAIL that exposed a driver-side missing `shift` putting
the timeout value into the remote argv). Every one of those runs recorded
whole evidence rows and destroyed its agent — including the trap-fired
destroys after driver exit.

---
ch:trace row=MSF-021 spec=docs/TEST-PLANE.md evidence=scripts/bunker-plane.sh + internal/plane/ + cmd/mischief/plane.go witness=live-run-2026-10-05
