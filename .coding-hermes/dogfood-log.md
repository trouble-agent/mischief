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

## 2026-10-10 — mischief-dogfood (serve/watchdog surface: first real use)

- Verdict: SHIPPABLE on the watchdog machinery itself; PROMISING-BUT-ROUGH on
  reachability — the flagship serve/TTL/kill-switch/reconcile surface WORKS
  (proven end-to-end) but no shipped verb can arm a hold, so a real user must
  hand-write reverter.jsonl to start the loop (DF-05).
- Angle: the surface no prior run touched (2026-10-08 covered CLI/install/plan;
  2026-10-10 02:30 sibling covered install leg + exit contracts). This run:
  `serve`, TTL expiry revert, kill switch under load, boot reconcile, revert
  honesty, journal corruption posture, perf.
- Promise tested: "the reverter is a watchdog that outlives the CLI: arm →
  land → hold under TTL → measured revert at expiry; serve owns TTLs, boot
  reconcile and health.json" (PRD §5/§6.1, SPEC-04).
- Real use (scratch dirs /tmp/dogfood-mischief*, all cleaned): authored the
  write-ahead arm record per the journal fold contract (hold_id = sha256
  content id; boot id MUST be the reverter id from boot.json, not the host
  boot id — the host-id variant grades false-stuck, DF-06). Armed hold:
  status shows armed + ttl_remaining; health.json shows armed with
  ttl_remaining_sec 6.97; daemon reverted at expiry with role=owner
  reason=ttl_expiry detail=check: same and the faulted file came back
  byte-identical ("original-state-v1"). Kill switch: 2 holds reverted wall=7ms
  (budget 2s) under active junk-writer load, rc=0. revert_failed honesty: an
  inverse whose backup is missing grades outcome=revert_failed detail=inverse
  failed: read backup: ... no such file; CLI exit 1; the fault stays live and
  the journal says so — exactly the honesty ladder working. Corrupt journal
  line: status, serve AND revert --all all fail loudly exit 1 (no silent skip).
  Foreign-boot arm grades stuck (safe direction; kill switch reverts it).
- Perf (Step 2b, measured, nothing worth a PERF row): status warm 5.2ms±1.9ms
  (hyperfine 20 runs, 1 armed + 4 reverted holds); serve cold boot to first
  health.json 29-31ms (3 runs); kill switch 7ms for 2 holds; TTL revert well
  inside its beat. Nothing a user would notice as slow.
- Findings filed: DF-05 (P2 no verb can arm a hold — Lifecycle.Arm is
  test-only; loop unreachable from the CLI), DF-06 (P2 boot-id adoption
  contract undocumented; host-boot-id arm record grades false-stuck on same
  boot), DF-07 (P3 run-dir default mismatch: CLI ~/.mischief/runs/default vs
  reverter doc.go/DefaultDir ~/.mischief/reverter).
- Install leg: SKIPPED-install-bunker NOT needed — the 02:30 sibling run
  PROVED the fresh-install premise on las-bunker-02 (agent 0c101d00) and this
  run adds no new install surface; re-proving it would duplicate DF-03.
- Foreman not woken (board-driven admission picks the rows up).
- Cleanup: all scratch dirs + scripts removed; no repo code touched.

## 2026-10-10 — mischief-dogfood-serve (second same-morning tick: prove, don't re-file)

- Sibling tick 7b50f11 landed DF-05/06/07 mid-run; this run RE-PROVED the DF-05
  premise independently from the daemon side (health.json holds:null through
  selftest --primitive F-009 + battery --quick on the same run dir, boot-id
  unchanged across kill+restart — hold-less by construction) instead of filing
  a duplicate. That re-proof is cited in DF-08's reasoning.
- NEW finding DF-08 (P2): battery --quick run-level verdict grades a clean
  named SKIP as 'flaky' (verdict/engine.go Aggregate disagrees across five
  DIFFERENT cells; a capability skip is not instability).
- Real-use evidence: bunker-las-02 agent b0089295, tar-over-ssh tree, go build
  18s cold, selftest F-009 PASS 1s (inode-identity proof), battery --quick 4
  pass/1 skip 1s, serve + health.json + status + revert --all all green.
- Perf (Step 2b): nothing a user would feel — build is the only multi-second
  operation (18s cold); no PERF rows, deliberately.
- Install leg: covered by DF-03 (proven 02:30 same morning, same premise, no
  new install surface) — cited skip, not silent.
- Cleanup: bunker agent destroyed via ttl; no repo code touched; board row
  DF-08 committed by sibling commit 7b50f11 (pathspec).

## 2026-10-10 — mischief-dogfood-serve (second same-morning tick: prove, don't re-file)

- Sibling tick 7b50f11 landed DF-05/06/07 mid-run; this run RE-PROVED the DF-05
  premise independently from the daemon side (health.json holds:null through
  selftest --primitive F-009 + battery --quick on the same run dir, boot-id
  unchanged across kill+restart — hold-less by construction) instead of filing
  a duplicate. That re-proof is cited in DF-08's reasoning.
- NEW finding DF-08 (P2): battery --quick run-level verdict grades a clean
  named SKIP as 'flaky' (verdict/engine.go Aggregate disagrees across five
  DIFFERENT cells; a capability skip is not instability).
- Real-use evidence: bunker-las-02 agent b0089295, tar-over-ssh tree, go build
  18s cold, selftest F-009 PASS 1s (inode-identity proof), battery --quick 4
  pass/1 skip 1s, serve + health.json + status + revert --all all green.
- Perf (Step 2b): nothing a user would feel — build is the only multi-second
  operation (18s cold); no PERF rows, deliberately.
- Install leg: covered by DF-03 (proven 02:30 same morning, same premise, no
  new install surface) — cited skip, not silent.
- Cleanup: bunker agent destroyed via ttl; no repo code touched; board row
  DF-08 committed by sibling commit 7b50f11 (pathspec).

## 2026-10-10 — mischief-chaos-dogfood (release-artifact real-use pass)

- Used the PUBLISHED v0.1.0 the way a user would: downloaded mischief_v0.1.0_linux_amd64.tar.gz
  + checksums.txt from GitHub releases into a clean /tmp dir; sha256 OK for amd64 (arm64
  FAILED-open warning = README-3, known/filed). Tarball ships mischief, libfault-anchored.so,
  README, LICENSE — NO catalog/.
- RE-PROOF of DF-02 (no re-file): from the extracted release dir, `selftest --primitive F-009`
  and `selftest --all` both fail `catalog/faults not found from cwd upward` — the released
  quickstart genuinely cannot run any catalog experiment; the artifact has no catalog to find.
- RE-PROOF of DF-01 (no re-file): `mischief plan` with no args prints usage and exits 0;
  `plan -f <malformed yaml>` prints a parse error and STILL exits 0. Silent-success on error
  confirmed on the release binary (repo HEAD already fixed doctor to exit 1 — DF-04 direction).
- Doctor on the release exits 1 on catalog load failure (consistent with DF-04's finding that
  the release behaves correctly there).
- Install leg: NOT skipped — clean-machine install from the published release PROVEN locally
  (extract + verify + run). Ephemeral-bunker leg covered by this morning's chaos tick
  20261010-0627 (PLANE pass, TEARDOWN-GONE agent 2004381c) — cited, not re-run.
- Perf (Step 2b): headline ops are fail-fast milliseconds — selftest 10-14ms, doctor instant,
  install --help instant, release download ~1s. Nothing a user would notice as slow; no PERF
  rows, deliberately.
- No new findings; no code or board rows touched. Log-only commit.

## 2026-10-10 — mischief-chaos-dogfood (chaos verb family: first real use)

- Angle: the surface no prior run touched. The 10-02/10-08/10-10 runs covered
  plan/selftest/doctor/install/serve/battery/plane — but the `chaos` verb
  family (check/matrix/finding, the M6 machinery docs/CHAOS-LANE.md teaches,
  and the reason this lane is named mischief-chaos) appears in no prior
  dogfood log, and the live lane's 10-06/10-10 runs drove `plane`, never the
  documented chaos loop. Binary: bin/mischief 14a3494 (ancestor of HEAD;
  HEAD delta is board/log/docs only).
- `chaos check` happy path: template instantiated per docs (scratch lane dir,
  board symlink to the primary's board) → contract OK + the exact symlink
  evidence line the doc promises, exit 0.
- Failure ladder (all refuse exit 2 with the doc's verbatim semantics):
  unsubstituted template, host=fleet-main-host, wrong lane name, board
  symlink as REAL directory (severed lane), missing symlink, dangling
  symlink. The isolation contract is real, not prose.
- `chaos matrix --project mischief --tier-max 1`: 5ms cold, derives 7/10
  cells included at L1; excluded cells carry named reasons (tier_above_ceiling
  with the numbers) — NULLs carry reasons, as promised. Artifact stamped
  mischief.chaos.matrix/v1 with catalog sha. Refusals: no catalog from cwd →
  exit 1; tier_max 9 → exit 2. Foreign project (crier) renders fine — correct,
  scope is still project (the guard lives on the LANE, validated by check).
- `chaos finding` on a board COPY (read-only lane; never touched the real
  board): files a valid board-vocabulary row, id content-addressed
  (MSF-CHAOS-aa9e2b7249ea); duplicate re-file refused (exit 1, "refuse, never
  duplicate", board byte-identical); priority P9 refused pre-write. The
  append-before-validate safety the doc claims held.
- FINDING (for the foreman; not filed — read-only lane): `chaos finding
  --verdict banana` is ACCEPTED and lands verdict:"banana" on the board, while
  --priority is vocabulary-validated. README promises "Verdicts are closed and
  honest" (recovered/degraded/.../void); the finding verb's verdict field is
  an open string and can put an off-vocabulary verdict on a live board.
  Suggested row: validate --verdict against the closed set (or the board's
  vocabulary) the way priority is, file direction P3.
- ADOPTION GAP (observation, not a code defect): no chaos-lane.yaml exists
  anywhere in the fleet stand-in dirs and no MSF-CHAOS-* row was ever filed —
  mischief's own -chaos lane has never run the documented chaos loop (it runs
  `plane` instead). MSF-017's promise is "each adopting project gets exactly
  ONE chaos satellite lane that runs the project's fault matrix"; mischief,
  the adopting project, does not yet do that. The machinery works (proven
  above); it is simply not wired into the lane's tick.
- Perf (Step 2b): chaos matrix 5ms cold (0.005s real), check/refusals
  instant, finding append instant. Nothing a user would wait on; no PERF rows,
  deliberately.
- Verdict: ✅ SHIPPABLE on this surface — the M6 contract does exactly what
  CHAOS-LANE.md says, refusals are honest, the matrix derivation is real, and
  the filing path is safe. One P3 finding (verdict vocabulary) + the wiring
  gap above.
- Artifacts: this log entry only (pathspec commit). Scratch evidence in
  /tmp/dogfood-mischief-chaos/ (board copy untouched at 78 lines incl. 2 probe
  rows; real board never written).
2026-10-12 (2nd tick) | sim surface driven on HEAD bfdffbd: 9/9 catalog I-shapes land rc=0 (15ms-2s; I-011 2s = intended stall), per-shape request logs written | findings: MISCHIEF-DF-14 (I-004 malformed-body fault unreachable, PaginationBug short-circuit) + MISCHIEF-DF-15 (AC-17 admitted path contradicted by unconditional 'refused' proof line + empty 'real: ' detail) | install leg: inherited from same-HEAD proof 0019233 (bunker-mvp agent df-mischief-1012, make bin 48s, smoke green) — no code change since | promise: catalog sim shapes are a working operator entry point — HELD for landing, BROKEN for I-004's promised detail and AC-17's admitted-path wording | commit d305575
