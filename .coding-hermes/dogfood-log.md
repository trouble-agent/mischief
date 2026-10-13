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
2026-10-12 (3rd tick) | lifecycle verb family (install/uninstall/audit/retention) driven report-only on HEAD bcefff5 (code unchanged from bfdffbd — board/log churn only): install --dry-run honest 3-action plan 8ms rc=0; audit surveys 9 verbs (8 sudo, 4 wildcard), correct NOT-granted posture, setuid sweep clean, rc=1 on uninstalled system (filed as drift-grading finding); retention whichever-first semantics PROVEN (count bound expires 6 of 26; age bound expires a 53-day-old dir within the count bound; both-unset fails closed rc=2); uninstall --dry-run integrates retention (aged run dir appears as a 4th action; --keep-days 999 drops it); --prefix honored for state but drop-in path fixed at /etc/sudoers.d/mischief (by-design, code-commented — filed as unprivileged-failure UX finding). Zero real data touched: all probes report-only/dry-run, live run-dir aged + restored in place, status re-read clean after | findings: MISCHIEF-DF-16 (P3: audit grades never-installed as drift, exit 1 + remediation names a README-undocumented verb) + MISCHIEF-DF-17 (P3: unprivileged --prefix install plan still writes privileged /etc drop-in, dry-run does not pre-screen the known blocker) | install leg: inherited — no code change since HEAD bfdffbd proof 0019233 (bunker-mvp agent df-mischief-1012, make bin 48s, smoke 3s green); PERF: every lifecycle op 7-9ms, nothing user-noticeable, no PERF row deliberately | promise: an operator can survey and manage mischief's privileged surface and run history from the CLI — HELD on every report-only path, ROUGH on first-run grading and unprivileged install UX | scratch evidence /tmp/dogfood-mischief-lifecycle/
2026-10-12 (4th tick) | chaos-lane adoption flow driven end-to-end for the first time (CHAOS-LANE.md section 1: sed template -> board symlink -> chaos check -> chaos matrix), HEAD f6f9929 (code unchanged from bfdffbd): happy path green rc=0 (template contract OK + symlink evidence line + matrix 7/10 included with named tier reasons); matrix leg clean on its own (missing --project rc=2, tier-max 0/9 rc=2 outside L0..L5, --markdown artifact written); control legs: relative primary_board refuses rc=2 (rule 6), battery bare invocation fail-closed rc=2 with guidance, battery --quick refuses fail-closed on unsanctioned host rc=2 (SPEC-13 held); finding F2 was USER ERROR — docs/CHAOS-LANE.md:36 defines --lane as <file>, 'open F: no such file' is correct fail-closed | findings: MISCHIEF-DF-18 (P1: chaos check passes a lane declaring host.name = the real scheduler hostname karaHermes-mde-7840hs — ValidateLane matches only the literal 'fleet-main-host' string, internal/chaos/lane.go:61, never os.Hostname(); green check + exit 0 on the isolation violation, only the runtime sanction gate would refuse later) + MISCHIEF-DF-19 (P2: chaos check never verifies the matrix artifact — tier-max 3 matrix in an L1 lane's workdir validates green, so the tick body can consume tier-3 cells on an L1-scoped lane) | install leg: inherited — no code change since HEAD bfdffbd proof 0019233 (bunker-mvp agent df-mischief-1012, make bin 48s, smoke 3s green, selftest 13/8/0) | PERF: chaos matrix + chaos check both 0.00s wall (sub-10ms), nothing user-noticeable, no PERF row deliberately | promise: an adopting project can wire a chaos satellite lane from the template and verify it with the CLI — HELD for the happy path, BROKEN for the isolation check's real-hostname case | scratch evidence /tmp/dogfood-mc-lane/ (all report-only: scratch lane dirs, scratch matrix out dirs, no board writes beyond DF-18/19, no fault landed)
2026-10-13 (5th tick) | code HEAD unchanged (b3789e6; only board/qa commits since) → UNEXERCISED-SURFACE sweep, not a re-audit: drove the two documented shipped surfaces no prior tick touched — `mischief retention` (SPEC-11 rotation) and the plane bookkeeping verbs `parse-agent-id`/`record`/`selftest-verdict` (MSF-021) | retention: 25 fake run dirs → dry-run plan names exactly the 5 expired (keep 20/30d), --apply removes exactly those (20 left), dry-run touches nothing, --keep-count 3 / --keep-days 5 overrides honored, 4ms warm — WORKS | plane verbs: parse-agent-id handles both spawn shapes + capacity-refusal → empty-id rc=0 (absence is data); record appends O_APPEND JSONL rows, bad status → rc=2; selftest-verdict pass/fail rc=0; fold: skip rc=3, fail rc=1, pass rc=0, empty file → named FAIL, unreadable file → named FAIL, 4ms warm | RE-PROOF on fresh evidence: MISCHIEF-PLANE-002 (corrupt line dropped, NOTE printed, still graded on survivors) and MISCHIEF-PLANE-001 (arbitrary stage name 'totally-not-a-stage' folds PASS — record never validates --step against the documented pipeline stages preflight|ship|selftest|collect|destroy) | NEW finding MISCHIEF-DF-20 (P3): fold's default pass reason asserts 'all stages green (agent obtained, selftest exit 0, evidence pulled, agent destroyed)' whenever the last step's detail is empty — a spawn-only evidence file with no selftest/collect/destroy rows prints that full claim, and the empty-detail pass row is indistinguishable from a genuinely complete run | install leg inherited from same-HEAD proof (bunker-mvp df-mischief-1012, 2026-10-12) — no code change since | PERF: retention + every plane verb 4ms cold-ish; nothing user-noticeable, no PERF row by the devalue rule | promise: bookkeeping verbs are honest about absence and corruption EXCEPT the fold default-reason overclaim — held with that one caveat | rows: MISCHIEF-DF-20 filed
2026-10-13 (5th tick) | docs surface driven as a real new-user walk (first run to do so), HEAD b3789e6, code unchanged since install proof 0019233 (bfdffbd): README docs table — all 6 targets exist on disk; FAULT-CATALOG.md census recomputed = 113 rows (P14 S14 R12 F16 N18 C12 I14 T4 = 104 primitives + 9 sim I-cases), EXACTLY matching the README's headline and per-layer claims, landed-proof + inverse columns present; HTML twins (FAULT-CATALOG/EXPERIMENTS/SPEC-PLAN) regeneration-current (git-log timestamps track their .md sources); EXPERIMENTS.md status table cross-checked against the four YAML headers — every runnable/rehearsal-only claim matches its header text, ex-002's wipe-live-state warning is stated in both docs and YAML | control legs: `mischief --help` rc=0 (5ms), selftest bare fails closed rc=2 with guidance, selftest --primitive P-001 on unsanctioned host refuses rc=2 SPEC-13 'nothing was written' | findings: MISCHIEF-DF-20 (P3: top-level --help verb list omits selftest — the README's M1-exit-criterion verb; distinct from CLI-001 exit codes and CLI-002 scratch-dir) | install leg: SKIPPED-install-bunker EXPLICIT — all 4 bunker servers unreachable this tick (las-02 rc=124 deadline, mvp registry empty, las-01/03/04 no response); code delta bfdffbd..HEAD on *.go/go.mod = EMPTY so proof 0019233 (bunker-mvp df-mischief-1012, make bin 48s, smoke green) remains valid inheritance | PERF (2b): help 0.00s ×3 cold+warm, selftest refusal 0.00s ×3, status 0.00s — nothing user-noticeable, no PERF rows deliberately | zero real data touched: read-only docs walk + report-only CLI probes; scratch snapshot /tmp/tasks-snap-df20.jsonl; board delta = DF-20 only | artifact: commit (see git log)
