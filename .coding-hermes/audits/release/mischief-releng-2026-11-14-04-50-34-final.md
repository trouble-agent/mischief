# mischief release-readiness sweep — mischief-releng-2026-11-14-04-50-34

Target: trouble-agent/mischief
Canonical checkout: /home/kara/mischief
Forge/client: GitHub / gh
Certified candidate: b0cac920d2208cba760edf392c7349a612a6703e
Remote/default parity: certified candidate `b0cac920d2208cba760edf392c7349a612a6703e` equals `origin/main` and `git ls-remote origin refs/heads/main`; the detached candidate is 0/0 against the fetched default branch.
Baseline: v0.1.0, peeled commit b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb.

## Pass-4 re-anchor (2026-11-15, daily readiness sweep)

Same lane, ~5 hours after pass 3. The carrier advanced from 819e691 to 4a7cf094ecc634b4b22bdf09f3e4dbe7952312a0 and the delta `819e691..4a7cf09` is exactly three files, all under `.coding-hermes/` (this artifact, `board/tasks.jsonl`, `board/events.jsonl`) — a **0 product-path change**. Pass-3's product measurement therefore carries; no new release decision is invented here, and the single rolling row RELEASE-013 is refreshed in place rather than duplicated.

Decision remains NO-CUT, on unchanged process gates plus one new product-grounds reason.

Census re-derived over the wider range at this tip: `v0.1.0..4a7cf09` is 567 commits including merges and 478 non-merge. Strict NUL-delimited conventional-subject census: feat 1, fix 19, perf 0, chore 12, docs 38, refactor 0, test 1, ci 1, other 406 — buckets sum to 478, so the inventory is fully accounted for. Typed-bang subjects 0 and line-start `BREAKING CHANGE:`/`BREAKING-CHANGE:` footers 0. The user-facing feature is still 0b52fb8 `feat(daemon): serve Prometheus metrics on a separately bound listener (MISCHIEF-OPS-001)`. Proposed bump stays v0.2.0 (minor); no major signal.

Exact-tip hosted CI is green: run 43683916602, workflow CI, push event, headSha 4a7cf094ecc634b4b22bdf09f3e4dbe7952312a0, completed/success. Its only job `Build & Test (1.24)` completed/success with 15 steps and 0 failed. Tag CI does not exist because no tag exists for v0.2.0.

Repository-owned release tooling was exercised fresh at this exact tip in a throwaway detached worktree (`/home/kara/.hermes/cache/scratch/releng-mischief-4a7cf09`, HEAD 4a7cf09, 0 dirty lines, go1.26.9): `go build ./...` rc=0, `go vet ./...` rc=0, `make release-dry` (goreleaser check, 1 configuration file validated) rc=0, `make dist` (goreleaser snapshot --clean) rc=0 producing linux amd64 and arm64 archives, `checksums.txt`, `artifacts.json`/`metadata.json` and both per-arch shim libraries. Snapshot assets carry the v0.1.0 name from the current tag — strategy evidence, not publishable assets.

New material read from the tip board this pass: the P0 product wave is mid-flight — `PROF-002` is `review` (three-value outcome everywhere; kill the false-green revert path) and `PROF-003` is `in_progress` (`mischief profile run`, the executor that actually applies a profile). A cut now would publish a half-landed P0 surface, which is a NO-CUT reason on product grounds independent of the process gates. The sixth dogfood fire (log at HEAD 9e99bae) also minted `MISCHIEF-DF-75` (metrics result vocabulary omits `not_landed` until observed) and `MISCHIEF-DF-76` (a pending row's acceptance criterion cites a `health` verb the binary does not ship), and `DOC-27`/`DOC-28`/`DOC-29` remain open — two documented incident commands exit 2 as printed. Documentation ships with a release, so those are release-facing.

## Pass-4 evidence table

| Surface | Evidence | Result |
|---|---|---|
| Candidate parity | `git rev-parse HEAD origin/main` = both 4a7cf094ecc634b4b22bdf09f3e4dbe7952312a0; `git status --porcelain` on the canonical checkout 0 lines before and after the fetch; `git pull --ff-only` fast-forwarded 819e691 → 4a7cf09 | PASS |
| Carrier delta | `git diff --name-status 819e691..4a7cf09` = 3 paths, all under `.coding-hermes/` (audit artifact, `board/tasks.jsonl`, `board/events.jsonl`); product-path change count 0 | PASS (measurement carried) |
| Commit inventory | `git rev-list --count` 567 / `--no-merges --count` 478 over `v0.1.0..4a7cf09`; feat 1, fix 19, perf 0, chore 12, docs 38, refactor 0, test 1, ci 1, other 406 (sum 478); typed-bang 0, line-start BREAKING footers 0 | v0.2.0 proposal |
| Exact-tip CI | Run 43683916602, workflow CI, event push, headSha 4a7cf094ecc634b4b22bdf09f3e4dbe7952312a0, completed/success; job `Build & Test (1.24)` completed/success, 15 steps, 0 failed | PASS |
| Build at exact tip | Throwaway detached worktree at 4a7cf09, 0 dirty, go1.26.9: `go build ./...` rc=0; `go vet ./...` rc=0 | PASS |
| Release rehearsal | Same worktree: `make release-dry` → goreleaser check, 1 config validated, rc=0; `make dist` → snapshot rc=0, linux_amd64 + linux_arm64 archives (3.6 MB / 3.3 MB), `checksums.txt`, `artifacts.json`, `metadata.json`, per-arch shim libs. Snapshot-only, not publishable | PASS strategy evidence |
| Release surface | `git ls-remote --tags origin` → only annotated `v0.1.0` (tag object af9a38f, peeled b123b4e); `gh release list` → only published `v0.1.0` (2026-10-07) | PASS for namespace availability; v0.2.0 absent by design |
| GitReins | `git ls-tree -r --name-only 4a7cf09 -- .gitreins` empty: no tracked config, no candidate-bound verdict in the candidate tree | UNVERIFIED / blocker (unchanged) |
| Blockers at tip | QA-MISCHIEF-002-R2 `failed` P1 (last updated 2026-10-29); QA-MISCHIEF-CHAOS-4 `pending` P3 phantom target; INT-CI-10 `pending` P3 (act runner-env, exitcode 127); QA-MISCHIEF-FOREMAN-1 `in_progress` P1; QA-MISCHIEF-FOREMAN-2 `in_progress` P2 | BLOCKING (unchanged) |
| Product in flight | PROF-002 `review` P0 (three-value outcome); PROF-003 `in_progress` P0 (`mischief profile run` executor) | BLOCKING (new reason) |
| Documentation defects | DOC-27 P2, DOC-29 P2 (two documented incident commands exit 2), DOC-28 P3, MISCHIEF-DF-75 P3, MISCHIEF-DF-76 P3 — all `pending` | RELEASE-FACING / open |
| Deployed binary | `/home/kara/.local/bin/mischief` still reports v0.1.0 (built 2026-10-16); predates the candidate | STALE |
| Rolling row | RELEASE-013 refreshed in place (`pending` P1) with candidate 4a7cf09, CI 43683916602 and this artifact path; no duplicate readiness row minted | PASS |

## Pass-3 re-anchor (2026-11-15)

The prior pass-2 artifact named 4d93164 as its live tip and d6d6059 as its certified candidate. The fetched default branch now certifies 819e6913509703fa493e0d9e2ddc40f2bd214171; this pass refreshes the header and records the newly measured state.

Decision remains NO-CUT. The proposed bump is v0.2.0 (minor): the range v0.1.0..819e691 contains 565 commits including merges and 476 non-merge commits. Strict conventional-subject census: feat 1, fix 19, perf 0, chore 12, docs 38, refactor 0, test 1, ci 1, other 404; typed-bang subjects 0 and BREAKING CHANGE footers 0. The user-facing feature remains 0b52fb8 `feat(daemon): serve Prometheus metrics on a separately bound listener (MISCHIEF-OPS-001)`; no major signal.

Exact-head hosted CI is green: GitHub run 43659623328, workflow CI, push event, headSha 819e6913509703fa493e0d9e2ddc40f2bd214171, completed success. Its only job `Build & Test (1.24)` completed success with every listed step successful, and the commit has 1 successful check-run and 1 successful check-suite. The release workflow is tag-triggered, so no v0.2.0 tag CI exists because no tag was created.

Repository-owned tooling was exercised from a clean detached checkout at the certified candidate: `make release-dry` rc=0; `make dist` rc=0. The snapshot produced Linux amd64 and arm64 archives, checksums, and per-arch shim libraries. These are snapshot strategy evidence, not publishable assets.

The release gate remains blocked by the existing rolling row RELEASE-013: QA-MISCHIEF-002-R2 is failed P1; QA-MISCHIEF-CHAOS-4 is pending with a phantom scheduler target; QA-MISCHIEF-FOREMAN-1 is in progress; candidate-bound GitReins remains unverified because `.gitreins/config.yaml` and history verdicts are absent from the candidate tree; and `/home/kara/.local/bin/mischief` remains the v0.1.0 binary built from b123b4e. The latest clean-machine evidence recorded in the board remains partial and includes an INT-CI-10 runner-environment failure, so it does not clear the gate. No release-cut authorization row naming this repository and v0.2.0 was found; the existing rolling row explicitly remains NO-CUT.

## Evidence table

| Surface | Evidence | Result |
|---|---|---|
| Candidate parity | `git rev-list --left-right --count HEAD...origin/main` = `0 0`; HEAD and origin/main = `819e6913509703fa493e0d9e2ddc40f2bd214171`; tree object `5c57f18c62a3003407b94686940ec14215aee43a` | PASS |
| Commit inventory | `v0.1.0..819e691`: 565 total / 476 non-merge; feat 1, fix 19, perf 0, chore 12, docs 38, refactor 0, test 1, ci 1, other 404; product-path diff excluding `.coding-hermes/**` and `.gitreins/**` has 208 changed paths | v0.2.0 proposal |
| Exact-head CI | Run 43659623328; exact `headSha` 819e691; completed success; job `Build & Test (1.24)` and all listed steps success; check-runs=1 success, check-suites=1 success | PASS |
| Recent CI history | `gh run list --repo trouble-agent/mischief --limit 20`: latest run 43659623328 and all 20 listed runs completed success on their own SHAs | PASS for main; tag CI not applicable |
| Release tooling | `docs/RELEASE.md`, `Makefile` targets `release-dry`/`dist`/`tags`, `.goreleaser.yml`, tag-triggered `.github/workflows/release.yml`; repository-owned path used, no generic fallback | PASS |
| Release rehearsal | Clean detached checkout at 819e691: `make release-dry` rc=0; `make dist` rc=0; outputs include `mischief_v0.1.0_linux_amd64.tar.gz`, `mischief_v0.1.0_linux_arm64.tar.gz`, `checksums.txt`, and both per-arch shim `.so` files; snapshot metadata reports synthetic snapshot provenance | PASS strategy evidence |
| GitReins | Working tree contains untracked local `.gitreins/` files, but the candidate tree has zero tracked `.gitreins` paths, no `.gitreins/config.yaml`, and no candidate-bound history verdict | UNVERIFIED / blocker |
| Dogfood / clean machine | Latest durable board evidence: 2026-11-14 battery on ancestor 933233a with zero product-path drift to prior candidate, partial cell set including 16 OK, 5 N/A, 2 INFO, 1 PASS, 1 UNVERIFIED, 1 FAIL owned by INT-CI-10; the latest dogfood log also records a 2026-11-14 fresh ephemeral install leg with native tests OK but act `ci-pass` UNVERIFIED. Neither is a complete candidate-bound release battery | PARTIAL / blocker |
| Deployed binary | `/home/kara/.local/bin/mischief`, mtime 2026-10-16 09:31:43 -0500, reports `mischief v0.1.0`, `vcs.revision=b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb`, `vcs.modified=false` | STALE |
| Release surface | Remote tag list contains only annotated `v0.1.0` peeled to b123b4e; `gh release list` contains only published v0.1.0; no v0.2.0 ref or Release object | PASS for namespace availability |
| Version hygiene | No `CHANGELOG.md` and no `Unreleased` heading; v0.1.0 references are historical release examples. A future v0.2.0 prep should review release-facing version claims before tagging | PREP / review needed |
| Open blockers | RELEASE-013 pending P1; QA-MISCHIEF-002-R2 failed P1; QA-MISCHIEF-CHAOS-4 pending P3; QA-MISCHIEF-FOREMAN-1 in progress P1; PROF-002 review P0; PROF-003 in progress P0; additional PROF P1 work remains open | BLOCKING |

## Known issues

- QA-MISCHIEF-002-R2 remains failed P1: fresh JIT-agent TTL-owner recurrence is unresolved; its evidence is not a candidate-bound release pass.
- QA-MISCHIEF-CHAOS-4 remains pending: six enabled scheduler satellites still point at the nonexistent `local:/home/kara/mischief-chaos` / `/home/kara/wojons/mischief-chaos`; this is an owner-gated scheduler/config defect outside the product tree.
- QA-MISCHIEF-FOREMAN-1 remains in progress: act battery evidence is not a substitute for hosted exact-head CI.
- Candidate-bound GitReins is unavailable: `.gitreins/config.yaml` and history verdicts are not in the candidate tree; the local untracked `.gitreins` files were not treated as release evidence.
- `/home/kara/.local/bin/mischief` remains a stale v0.1.0 deployment and must be refreshed by its owner after a future cut.

## Unblock

1. Resolve QA-MISCHIEF-002-R2 with a fresh JIT-agent receipt that records the failing/passing cells and mechanism, or an explicit accepted ruling that the existing recurrence is not release-blocking.
2. Correct the scheduler database and regenerate the fleet mirror for QA-MISCHIEF-CHAOS-4, then prove a bare-dispatched tick resolves `/home/kara/mischief` without hand-exported target variables.
3. Obtain candidate-bound GitReins history or an explicitly accepted alternative exact-candidate gate.
4. Re-run releng at a stable candidate; only then follow `docs/RELEASE.md` to create the annotated v0.2.0 tag and let tag CI/release workflow produce the draft for review. Do not use `gh release create` fallback while the repo-owned flow exists.

No tag, GitHub Release, artifact publication, deployed-binary mutation, or force-move was performed by this pass.

## Pass-6 re-anchor (2026-11-15, current delivery)

The previous pass certified `1126bcbea56cd86958816c148e828e09bae89181`. The fetched canonical default-branch tip is now `edd03537bbaeb04ff50fbad895d802dcd86ebd47`; the delta is three paths, all under `.coding-hermes/` (the release artifact, board events, and the rolling task row). The product tree is unchanged, so pass-5 product measurements carry; the candidate and hosted CI receipt are refreshed to the new exact tip.

Decision remains NO-CUT. The proposed version remains `v0.2.0` (minor): no breaking marker was found in the carried census, and the single user-facing `feat(daemon)` Prometheus metrics change still drives the minor proposal. No major confirmation is needed.

## Pass-6 evidence table

| Surface | Evidence | Result |
|---|---|---|
| Candidate identity | `git rev-parse origin/main` = `edd03537bbaeb04ff50fbad895d802dcd86ebd47`; `git ls-remote --heads origin refs/heads/main` agrees; product-path delta from `1126bcb` is 0 | PASS |
| Candidate movement | `1126bcb..edd0353` changes only `.coding-hermes/audits/release/mischief-releng-2026-11-14-04-50-34-final.md`, `.coding-hermes/board/events.jsonl`, and `.coding-hermes/board/tasks.jsonl` | BOOKKEEPING-ONLY; pass-5 product evidence carried |
| Commit inventory | Carried from exact product tree at `1126bcb`: 578 total / 487 non-merge; strict conventional census has `feat=1`, `fix=20`, `perf=0`, `chore=13`, `docs=40`, `refactor=0`, `test=1`, `ci=1`, `other=409`; typed-bang=0; line-start BREAKING CHANGE footers=0 | v0.2.0 proposal |
| Exact-tip hosted CI | GitHub run `43687404249`, workflow `CI`, push event, exact `headSha=edd03537bbaeb04ff50fbad895d802dcd86ebd47`, completed/success; `Build & Test (1.24)` completed with 15/15 steps successful and 0 failed/skipped | PASS |
| Local required gates | Carried from the unchanged product tree at `1126bcb`: `go build ./...` rc=0, `go vet ./...` rc=0, `make release-dry` rc=0, `make dist` rc=0 with Linux amd64/arm64 snapshot archives, checksums, and per-arch shims | PASS strategy evidence |
| Local full tests | Carried failure at `1126bcb`: `go test -count=1 ./...` rc=1 in `internal/chaos` (`TestSymlinkEvidenceOK`, `TestVerifyAgainstMatches` resolve `/var/tmp/hermes-go-tmp-real` instead of `/var/tmp/hermes-go-tmp`); attribution is not cleared by the bookkeeping-only move | FAIL / release blocker |
| GitReins | `git ls-tree -r --name-only edd0353 -- .gitreins` is empty; no candidate-bound config/history verdict exists in the candidate tree | UNVERIFIED / blocker |
| Dogfood / QA | Existing rolling evidence remains partial: failed P1 `QA-MISCHIEF-002-R2`, pending phantom-target `QA-MISCHIEF-CHAOS-4`, in-progress `QA-MISCHIEF-FOREMAN-1/2`; latest dogfood has act `ci-pass` UNVERIFIED and no complete candidate-bound release battery | BLOCKING |
| Product work | `PROF-002` remains `review` P0; `PROF-003`, `PROF-011`, `PROF-012`, and `PROF-013` remain in progress | BLOCKING |
| Release tooling | Repository-owned `docs/RELEASE.md`, `Makefile` targets `release-dry`/`dist`/`tags`, `.goreleaser.yml`, and tag-triggered `.github/workflows/release.yml` are present; use this path, not `gh release create` fallback | PASS |
| Release surface | Remote has only annotated `v0.1.0` peeled to `b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb`; `gh release list` shows one published release; `v0.2.0` tag and Release are absent | PASS / unchanged |
| Deployed binary | `/home/kara/.local/bin/mischief` mtime `2026-10-16`, reports `mischief v0.1.0`; it predates the candidate | STALE / blocker |
| Version hygiene | No `CHANGELOG.md` or `Unreleased` heading; release contract is documented, but future v0.2.0 version-facing claims still need review | PREP needed |

## Known issues

- `QA-MISCHIEF-002-R2` remains failed P1: the fresh JIT-agent TTL-owner recurrence is unresolved.
- `QA-MISCHIEF-CHAOS-4` remains pending: scheduler satellites still point at the nonexistent `mischief-chaos` path; this is outside the product tree.
- `QA-MISCHIEF-FOREMAN-1/2` remain in progress; act or stale premise evidence does not replace candidate-bound release evidence.
- Candidate-bound GitReins is unavailable; historical/local untracked verdicts are not evidence for `edd0353`.
- `PROF-002`/`PROF-003`/`PROF-011`/`PROF-012`/`PROF-013` remain unfinished product work.
- The deployed `/home/kara/.local/bin/mischief` remains the v0.1.0 binary.
- The required local full-suite failure remains unresolved or unattributed in a clean environment.

## Unblock

1. Reproduce or cleanly attribute the `internal/chaos` symlink-root test failure and obtain a fresh full-suite pass.
2. Resolve `QA-MISCHIEF-002-R2` with a fresh JIT-agent receipt or an explicitly accepted release ruling.
3. Correct the scheduler database and mirror for `QA-MISCHIEF-CHAOS-4`, then prove a bare-dispatched tick resolves `/home/kara/mischief`.
4. Obtain candidate-bound GitReins evidence or an explicitly accepted equivalent gate.
5. Finish/reconcile the open P0/P1 product rows and rerun releng at a stable candidate; only then follow `docs/RELEASE.md` for the annotated tag and tag-triggered draft Release.

## Decision

NO-CUT. Do not create or move `v0.2.0`, do not create a GitHub Release, do not mutate deployed artifacts, and do not force-move any tag. Exact-tip hosted CI is green, but the release gate is not: the unchanged product tree carries a failed required local full suite, candidate-bound GitReins is unavailable, QA/dogfood evidence is incomplete, open P0/P1 work remains, and the deployed binary is stale.

No tag, GitHub Release, artifact publication, deployed-binary mutation, or force-move was performed by this pass.

## Pass-2 re-anchor (same tick, second pass)

The header parity lines above were true at pass 1 and aged by this artifact's own carrier commit; they are superseded by this block, not wrong. Live tip at pass 2 is `4d931642d165ed291d9dd7db2a6dd83d53751a7b`. The delta `d6d605929a50071524b543016e7dc8a9052a1439..4d93164` is exactly one file — this artifact — with 0 product-path changes. Exact-tip CI run 43559322323 (workflow CI, push, headSha 4d93164, completed success) covers it; pass-1 CI run 43541516784 was green on d6d6059.

New evidence re-derived at pass 2 (available but not recorded at pass 1): QA tick `mischief-chaos-qa-2026-11-14-00-58-14` ran a fresh clean-machine battery on tree `933233abf23a06c6d1015c61d0f4bd0486afadd9`, which is an ancestor of the live tip and differs from it by 0 product-path changes — so the candidate product tree has been exercised on a clean agent. That battery is partial, not a release battery: 16 OK / 5 N/A (product-absent) / 2 INFO / 1 PASS / 1 UNVERIFIED / 1 FAIL, the single FAIL being act Main Test exit 1 owned by INT-CI-10 (runner-env defect, row still pending). It does not clear the gate but it upgrades "no candidate-bound clean-machine evidence" to "partial coverage of an identical product tree".

Decision unchanged: NO-CUT. v0.2.0 stays uncut until QA-MISCHIEF-002-R2, QA-MISCHIEF-CHAOS-4, INT-CI-10, QA-MISCHIEF-FOREMAN-1/2 reach terminal state and candidate-bound GitReins or an accepted equivalent gate exists.

## Decision

NO-CUT — do not create or move v0.2.0, do not create a GitHub Release, and do not mutate deployed artifacts. The candidate has a minor-sized user-facing feature and green exact-head main CI, but release-grade evidence is incomplete: no GitReins verdict is bound to d6d6059, the latest clean-machine evidence is not bound to this candidate and the latest recorded dogfood is only partial/rough, and the deployed binary remains v0.1.0. Release CI is tag-triggered and unproven until the cut; the repository-owned flow requires the tag to be pushed only after readiness gates are proven.

Semver proposal: v0.2.0 (minor). The v0.1.0..candidate range contains 548 commits including merges and 459 non-merge commits. Strict NUL-delimited conventional census over the exact range: feat 1, fix 19, chore 12, docs 31, test 1, other/non-conventional 395; perf 0, refactor 0, typed ! 0, BREAKING CHANGE footers 0. The feature is 0b52fb8 `feat(daemon): serve Prometheus metrics on a separately bound listener (MISCHIEF-OPS-001)`. No major signal.

## Evidence table

| Surface | Evidence | Result |
|---|---|---|
| Candidate parity | `git rev-list --left-right --count origin/main...HEAD` = `0 0`; all refs resolve to d6d605929a50071524b543016e7dc8a9052a1439 | PASS |
| Commit inventory | `v0.1.0..origin/main`: 548 total / 459 non-merge; feat 1, fix 19, perf 0, breaking 0; changed product paths include PROF-011/012/013 and P-020 work plus the metrics feature | v0.2.0 proposal |
| Exact-head CI | GitHub run 43541516784, workflow CI, event push, exact headSha d6d605929a50071524b543016e7dc8a9052a1439, completed success. The job id was not separately captured; `Build & Test (1.24)` had 15/15 steps success, 0 failed, 0 skipped, including build, vet, docs twins, tests, selftest, N-012 battery, per-arch shim live proof, shim drift, and row-status drift | PASS |
| Recent CI history | `gh run list --repo trouble-agent/mischief --limit 20` returned the current run and preceding exact-SHA push runs; current and preceding listed runs were completed success on their own SHAs | PASS for main; tag CI not yet applicable |
| Repository release tooling | `docs/RELEASE.md`, Makefile `release-dry`/`dist`/`tags`, `.goreleaser.yml`, and tag-triggered `.github/workflows/release.yml`; tag workflow reruns gates and creates a DRAFT GitHub Release for review | PASS for tooling presence |
| Release rehearsal | Disposable full clone at d6d6059: `make release-dry` rc=0; `make dist` rc=0; produced linux amd64/arm64 snapshot archives, checksums, and per-arch shim libraries. Snapshot stamp is synthetic and assets are not publishable | PASS strategy evidence |
| GitReins | `.gitreins/config.yaml` absent; no stored verdict file matched d6d605929a50071524b543016e7dc8a9052a1439; available recent verdicts include FAIL/INCOMPLETE and are bound to older SHAs | UNVERIFIED / blocker |
| Dogfood / clean machine | Latest repo log records a 2026-10-31 clean-machine leg as PARTIAL / harness-blocked-unverified: no eligible isolated host, selftest/battery load-gated; prior fresh-install evidence is at older candidates and prior clean-agent TTL-owner failures remain owned by QA rows | UNVERIFIED / blocker |
| Deployed binary | `/home/kara/.local/bin/mischief`, mtime 2026-10-16 09:31:43 -0500, size 7962808, reports `mischief v0.1.0`; it predates the candidate | STALE |
| Release surface | Remote has only annotated tag v0.1.0 and one published non-draft Release with checksums.txt plus linux amd64/arm64 archives; v0.2.0 tag/release absent | PASS for namespace availability |
| Version hygiene | No CHANGELOG.md or Unreleased section. README, SECURITY, skills, and release docs intentionally contain v0.1.0 references for the previous release; no candidate version-prep surfaces were changed. The repo's release doc owns the tag flow, but next cut should refresh release-facing current-version claims if they are intended to track latest | PREP / review needed |

## Known issues

- QA-MISCHIEF-002-R2 / QA-MISCHIEF-007: prior clean-agent TTL-owner recurrence remains an open release-gating QA class in the board history; current candidate-bound clean-machine proof is absent.
- QA-MISCHIEF-CHAOS-4: the QA dispatch target is a phantom `/home/kara/wojons/mischief-chaos` / `local:/home/kara/mischief-chaos` path, while the canonical repository is `/home/kara/mischief`.
- QA-MISCHIEF-FOREMAN-1: act-based CI battery evidence is open/in progress and is not a substitute for hosted exact-head CI.
- QA-MISCHIEF-FOREMAN-2: its title says v0.1.0 does not build, but the prior release built successfully in the earlier release record; premise requires reconciliation before reuse.
- Deployed `/home/kara/.local/bin/mischief` remains v0.1.0 and must be refreshed by its owning deployment path after a future cut.

## Unblock

1. Produce candidate-bound GitReins evidence or an accepted project-specific replacement exact-head gate; do not treat older PASS/FAIL verdicts as evidence for d6d6059.
2. Run and record a fresh clean-machine QA/dogfood battery against d6d605929a50071524b543016e7dc8a9052a1439, resolving the TTL-owner recurrence and phantom target path. A partial, load-gated, or harness-setup result does not clear the gate.
3. Re-run the readiness sweep after those receipts and verify the candidate has not moved. If all gates are terminal green, cut only through `docs/RELEASE.md`: annotated v0.2.0 tag push, tag CI, DRAFT Release review, then publish/verify the complete asset set.

No tag, GitHub Release, artifact publication, deployment restart, or force-move was performed by this sweep.

## Pass-5 re-anchor (2026-11-15, current delivery)

The prior pass certified `4a7cf094ecc634b4b22bdf09f3e4dbe7952312a0`. The fetched canonical tip is now `1126bcbea56cd86958816c148e828e09bae89181`, a six-commit advance. The product-path delta is non-empty, so prior measurements are not silently carried. The new product changes are `README.md`, `cmd/mischief/arm.go`, `cmd/mischief/arm_test.go`, `docs/OPERATIONS.md`, `internal/daemon/daemon_test.go`, `internal/reverter/lifecycle.go`, `internal/reverter/prof002_not_landed_test.go`, and `internal/reverter/record.go`; board, wave, and release-audit files also moved.

Decision remains NO-CUT. The proposed version remains `v0.2.0` (minor): the exact range `v0.1.0..1126bcbea56cd86958816c148e828e09bae89181` contains 576 commits, 485 non-merge commits, and 91 merge commits. Strict subject census over the 485 non-merges: feat 1, fix 20, perf 0, chore 13, docs 40, refactor 0, test 1, ci 1, other 409; the buckets sum to 485. Typed-bang subjects: 0. Line-start BREAKING CHANGE/BREAKING-CHANGE footers: 0. The user-facing feature remains `0b52fb8 feat(daemon): serve Prometheus metrics on a separately bound listener`; no major signal.

## Pass-5 evidence table

| Surface | Evidence | Result |
|---|---|---|
| Candidate parity | `git rev-parse HEAD origin/main` = both `1126bcbea56cd86958816c148e828e09bae89181`; canonical tree clean | PASS |
| Candidate movement | `4a7cf09..1126bcb` includes eight product files plus four bookkeeping paths | MATERIAL — measurements re-derived |
| Exact-tip hosted CI | GitHub run `43685673677`, workflow `CI`, push event, exact `headSha=1126bcbea56cd86958816c148e828e09bae89181`, completed/success. Job `Build & Test (1.24)` ran 15 successful steps, 0 failed, including build, vet, docs twins, test, selftest, N-012 battery, shim smoke/drift, and row-status drift | PASS |
| Local clean candidate gates | Detached worktree at exact `1126bcbea56cd86958816c148e828e09bae89181`, status 0; `go build ./...` rc=0; `go vet ./...` rc=0; `make release-dry` rc=0 (1 config validated); `make dist` rc=0 (Linux amd64/arm64 snapshot archives, checksums, per-arch shims) | PASS |
| Local full tests | `go test -count=1 ./...` rc=1: `internal/chaos` fails `TestSymlinkEvidenceOK` and `TestVerifyAgainstMatches`; both resolve the symlink target under `/var/tmp/hermes-go-tmp-real` while the declared primary is under `/var/tmp/hermes-go-tmp`. This is a local scratch-root collision/fixture-environment failure, not evidence that the exact-head hosted test passed the same path, but it is a failed required local gate and remains a release blocker pending clean-environment reproduction/attribution | FAIL / UNVERIFIED attribution |
| GitReins | 230 local verdict files exist under untracked `.gitreins/`, but `git ls-tree -r --name-only HEAD -- .gitreins` is empty and no verdict was found bound to `1126bcbea56cd86958816c148e828e09bae89181`; candidate-bound GitReins remains UNVERIFIED | BLOCKING |
| Dogfood / QA | Board retains failed P1 `QA-MISCHIEF-002-R2`, pending phantom-target `QA-MISCHIEF-CHAOS-4`, and in-progress `QA-MISCHIEF-FOREMAN-1/2`; latest durable dogfood is partial with act `ci-pass` UNVERIFIED. No candidate-bound clean-machine release battery is present | BLOCKING |
| Product work | `PROF-002` is `review` P0 and `PROF-003` is `in_progress` P0; `PROF-011`, `PROF-012`, and `PROF-013` remain in progress. The candidate still contains unfinished P0/P1 product surfaces | BLOCKING |
| Release tooling | Repo-owned `docs/RELEASE.md`, `Makefile` (`release-dry`, `dist`, `tags`), `.goreleaser.yml`, and tag-triggered `.github/workflows/release.yml` are present and were used. No `gh release create` fallback is appropriate | PASS |
| Release surface | Remote has only annotated `v0.1.0` (peeled `b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb`) and one published non-draft Release. `v0.2.0` tag ref is absent. No tag or Release was created | PASS / unchanged |
| Deployed binary | `/home/kara/.local/bin/mischief` reports `mischief v0.1.0`, mtime 2026-10-16; its installed commit is the prior release ancestor, so it is stale | BLOCKING / owner action |
| Version hygiene | No `CHANGELOG.md` or `Unreleased` heading. The repository's release contract is documented, but a future v0.2.0 prep must review release-facing version claims | PREP needed |
| Known issues | `QA-MISCHIEF-002-R2`, `QA-MISCHIEF-CHAOS-4`, `QA-MISCHIEF-FOREMAN-1/2`, candidate-bound GitReins, unfinished P0/P1 work, stale deployed v0.1.0, and the local full-test scratch-root failure above | BLOCKING |

## Decision

NO-CUT. Do not create or move `v0.2.0`, do not create a GitHub Release, do not mutate deployed artifacts, and do not force-move any tag. Main CI is green, but the release gate is not: the local full suite fails, candidate-bound GitReins is unavailable, open P0/P1 work and QA blockers remain, dogfood evidence is partial, and the deployed binary is stale. If the local `internal/chaos` failures are later attributed to the scratch-root setup, they still require a clean rerun before release; hosted success does not erase a failed required local gate.

The rolling board row `RELEASE-013` remains the single release-readiness row. No duplicate RELEASE row was created. This pass updates its evidence only after the final candidate and artifact bytes are fixed. No tag, GitHub Release, package publication, deployment mutation, or force-move was performed.

## Pass-7 re-anchor (2026-11-15, tick `mischief-releng-2026-11-15-05-54-11`)

Pass 6 certified `edd0353`. The fetched canonical tip is now `a3bf6ec649ac72bd82159d23ddd76ff85623e5b3`, a five-commit advance, and the product-path delta is non-empty, so every measurement below is re-derived rather than carried. Product changes since pass 6: `cmd/mischief/metrics_test.go`, `docs/OPERATIONS.md`, `internal/daemon/metrics.go`, `internal/reverter/record.go`, `README.md` (MISCHIEF-DF-75 not_landed series plus DOC-27/DOC-29); the rest is board, wave, and release-audit bookkeeping.

Decision is unchanged, **NO-CUT for v0.2.0**, but two of pass 6's stated grounds are retired by measurement and replaced with narrower ones.

**Retired: the local full-suite failure is attributed, and gone.** `go test -count=1 ./...` at `a3bf6ec` exits 0 with 22 packages ok and 0 failures — but only with `GOTMPDIR` unset. This host exports `GOTMPDIR=/var/tmp/hermes-go-tmp`, which is a *symlink* to `/var/tmp/hermes-go-tmp-real` (created 2026-11-06 by host hygiene, not by any test). Go's `t.TempDir()` therefore hands `internal/chaos` a fixture root under the symlink, `SymlinkEvidence` legitimately resolves the board to the `-real` target, and the assertion compares it against the unresolved declared primary. Reproduced both ways at the same commit: `GOTMPDIR=/var/tmp/hermes-go-tmp go test ./internal/chaos/` fails `TestSymlinkEvidenceOK` and `TestVerifyAgainstMatches`; `env -u GOTMPDIR TMPDIR=<real dir> go test -count=1 ./...` is clean. Hosted CI is unaffected (ubuntu `/tmp` is real). This is a test-robustness defect, not a product red, and it is filed as `RELEASE-014` rather than carried as a release blocker.

**Falsified: `QA-MISCHIEF-FOREMAN-2`'s premise.** At `v0.1.0` (peeled `b123b4e`) `go build ./...` exits 0 on Go 1.26.9 and yields a working binary (unstamped, `mischief unknown`). The previous release was refused at the *release workflow* gates (`INT-CI-4`), not because its source does not build. The row's title premise should not be reused as a cut blocker.

**Still blocking.** No candidate-bound clean-machine QA/dogfood battery: the newest durable dogfood is 2026-11-14-2243 at `04e7bfc` (ancestor, three commits behind) with verdict *works, containment promise holds, but late and three doc/output mismatches* — a findings pass, not a release battery. `QA-MISCHIEF-002-R2` stays open P1: its named test is green locally three times over (`go test ./internal/reverter/ -run TestOwnerSurvivesKill9AndRevertsAtTTL -count=3` rc=0, 21.2s), which is consistent with the row's own claim that the tree is green on the dev box and in CI — the unverified half is the JIT-agent mechanism, and that is still unproven. `PROF-002` (P0) sits in `review`, `PROF-003` (P0) in `in_progress`; `mischief profile` is still an unknown verb and no doc claims it, so nothing false is promised, but the candidate ships unfinished P0 work. `QA-MISCHIEF-CHAOS-4` still names a phantom target, `QA-MISCHIEF-FOREMAN-1` is still in progress.

**GitReins.** The repository tracks no `.gitreins` path; the checkout carries an untracked `.gitreins/` with 231 verdict files. The freshest is `2026-11-15T09:43:06Z` on `742166fd` (two commits behind the candidate) for `MISCHIEF-DF-75`: `merge_recommendation=no_concerns_found` with all five items affirmative, including a mutation check that removing `OutcomeNotLanded` from `DeclaredOutcomes` fails `metrics_test.go:104` and a live scrape confirming four series — yet the artifact's own top-level `passed` flag is `false`. That self-inconsistency is recorded here rather than read as either PASS or FAIL. No verdict is bound to `a3bf6ec`; candidate-bound GitReins remains absent.

## Pass-7 evidence table

| Surface | Evidence | Result |
|---|---|---|
| Candidate parity | `git rev-parse origin/main` = `a3bf6ec649ac72bd82159d23ddd76ff85623e5b3`, stable across two fetches 12 minutes apart; disposable worktrees at the exact sha report clean status | PASS |
| Candidate movement | `edd0353..a3bf6ec` = 5 commits, 5 product paths; prior measurements re-derived | MATERIAL |
| Commit census | `v0.1.0..a3bf6ec`: 589 total / 497 non-merge; feat 1, fix 20, docs 42, chore 13, test 1, ci 1, other lowercase prefixes 390 (tick titles), non-conventional 29; buckets sum to 497; typed-bang 0, line-start BREAKING footers 0 | v0.2.0 (minor) |
| Exact-tip hosted CI | run `43706150195`, workflow `CI`, event push, exact `headSha=a3bf6ec...`, completed/success; job `Build & Test (1.24)` 15/15 steps success including build, vet, docs twins, tests, selftest, N-012 battery, per-arch shim live proof, shim drift, row-status drift | PASS |
| Local gates at exact tip | detached worktree: `go build ./...` rc=0; `go vet ./...` rc=0; `make release-dry` rc=0; `make dist` rc=0; `tools/shim-build.sh --smoke` rc=0 with live landing proof on both arches (`arm64-landing-proof ok`, `hits=1`) | PASS |
| Local full suite | `env -u GOTMPDIR TMPDIR=<real> go test -count=1 ./...` rc=0, 22 packages ok, 0 fail. With the host `GOTMPDIR` symlink in place, `internal/chaos` fails two symlink-resolution tests — attributed, reproduced, and filed as RELEASE-014 | PASS / attributed |
| Release artifacts | snapshot archives contain `mischief`, `libfault-anchored.so` at 0755, `LICENSE`, `README.md` on amd64 and arm64; `checksums.txt` matches both | PASS |
| 002-R2 named test | `go test ./internal/reverter/ -run TestOwnerSurvivesKill9AndRevertsAtTTL -count=3` rc=0 (21.2s) | PASS locally / agent-side UNVERIFIED |
| GitReins | no tracked `.gitreins`; 231 untracked verdicts; freshest `2026-11-15T09:43:06Z` on `742166fd` (2 behind) = `no_concerns_found` with 5 affirmative items but `passed=false`; nothing bound to `a3bf6ec` | UNVERIFIED for candidate |
| Dogfood / QA | newest durable log 2026-11-14-2243 at `04e7bfc` (3 behind): works-with-findings; no candidate-bound clean-machine battery | BLOCKING |
| Product work | `PROF-002` review P0; `PROF-003` in_progress P0 (`mischief profile` = unknown verb, unclaimed in docs); `PROF-011/012/013` in progress | BLOCKING |
| Release surface | remote has annotated `v0.1.0` (peeled `b123b4e`) and one published Release; `v0.2.0` tag and Release absent; no tag was created or moved | PASS / unchanged |
| Version hygiene | no `CHANGELOG.md`; no stale candidate-version claims in the tree; README install pins name `v0.1.0`, correct until the next cut and a post-cut refresh item | PREP |
| Snapshot stamp | `make dist` at `a3bf6ec` stamps `mischief v0.1.0` into the binary and names archives `mischief_v0.1.0_linux_*.tar.gz`, while docs/RELEASE.md says the dry run stamps `v0.0.0` | DRIFT — RELEASE-015 |
| Deployed binary | `/home/kara/.local/bin/mischief` = `mischief v0.1.0`, mtime 2026-10-16, size 7962808, predates the candidate | STALE / owner action |

## Pass-7 unblock ladder

1. Run and record a clean-machine QA/dogfood battery bound to a fixed candidate sha; the 002-R2 acceptance asks specifically for the JIT-agent shape (recorded detach mode plus unit `ActiveState`/`ExecMainStatus`), not another dev-box rerun.
2. Land or explicitly defer the two open P0s; if `PROF-003` is deferred, say so on the row so the release does not ship a half-exposed surface.
3. Fix `RELEASE-014` so a symlinked temp root stops reddening the required local gate, and `RELEASE-015` so a snapshot build cannot wear a published tag.
4. Then re-run this sweep against a stable candidate and cut only through `docs/RELEASE.md`: annotated `v0.2.0` tag, tag CI, DRAFT Release review, publish, verify the full asset set, and refresh the deployed binary.

No tag, GitHub Release, artifact publication, deployment mutation, or force-move was performed by this pass.

## Pass-8 re-anchor (2026-11-15, tick mischief-chaos-releng-2026-11-15-11-18-38)

The prior certified product tree was `a3bf6ec649ac72bd82159d23ddd76ff85623e5b3`. The fetched canonical default-branch tip is `b0cac920d2208cba760edf392c7349a612a6703e`; its delta from `a3bf6ec` is exactly three `.coding-hermes/` paths (the prior release artifact and board files), with zero product-path changes. The previous product measurements therefore carry, while candidate CI and all release-surface readings were refreshed at the new tip.

## Decision

NO-CUT for proposed `v0.2.0`. The repository-owned release contract is present and the exact candidate CI and local rehearsal gates are green, but release-grade evidence is still incomplete and the existing rolling row remains an explicit NO-GO. No tag, GitHub Release, artifact publication, deployment mutation, or force-move was performed.

The semver proposal remains `v0.2.0` minor: `v0.1.0..b0cac920d2208cba760edf392c7349a612a6703e` contains 590 commits including merges and 498 non-merge commits. Strict conventional-subject classification over the captured non-merge subjects yields feat 1, fix 20, perf 0, chore 14, docs 42, refactor 0, test 1, ci 1, other conventional 390, and non-conventional 29; the buckets sum to 498. Typed-bang subjects are 0 and line-start BREAKING CHANGE footers are 0. No major confirmation is required.

## Evidence table

| Surface | Evidence | Result |
|---|---|---|
| Candidate identity | `origin/main`, `git ls-remote origin refs/heads/main`, and the detached candidate all resolve to `b0cac920d2208cba760edf392c7349a612a6703e`; the candidate is the GitHub default branch `main` | PASS |
| Candidate movement | `a3bf6ec..b0cac92` changes only `.coding-hermes/audits/release/mischief-releng-2026-11-14-04-50-34-final.md`, `.coding-hermes/board/events.jsonl`, and `.coding-hermes/board/tasks.jsonl`; product-path count 0 | BOOKKEEPING-ONLY; prior product measurements carried |
| Commit census | `v0.1.0..b0cac92`: 590 total / 498 non-merge / 92 merge; feat 1, fix 20, perf 0, chore 14, docs 42, refactor 0, test 1, ci 1, other conventional 390, non-conventional 29; typed-bang 0; BREAKING footers 0 | v0.2.0 proposal |
| Exact-tip hosted CI | GitHub run `43707532116`, workflow `CI`, push event, exact `headSha=b0cac920d2208cba760edf392c7349a612a6703e`, completed success. `Build & Test (1.24)` ran 15 successful steps, including build, vet, docs twins, test, selftest, N-012 battery, per-arch shim live proof, shim drift, and row-status drift | PASS |
| Local candidate gates | Clean detached candidate: `env -u GOTMPDIR go build ./...` rc=0; `go vet ./...` rc=0; `go test -count=1 ./...` rc=0 with 22 package results; `make release-dry` rc=0; `make dist` rc=0; `tools/shim-build.sh --smoke` rc=0 with amd64 and arm64 EIO/live-proof hits=1 | PASS strategy evidence |
| Release rehearsal | Snapshot produced both Linux archives, `checksums.txt`, metadata, and per-arch shim libraries. GoReleaser reports snapshot version `0.1.0-SNAPSHOT-b0cac92` while archive names follow the repository's current-tag-derived `v0.1.0` template; this is rehearsal evidence only and is the still-open RELEASE-015 documentation drift | PASS with non-blocking drift |
| Release surface | Remote has only annotated tag `v0.1.0`, peeled `b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb`, and one published non-draft GitHub Release. `v0.2.0` tag and Release are absent | PASS / namespace available |
| Repository-owned tooling | `docs/RELEASE.md`, Makefile `release-dry`/`dist`/`tags`, `.goreleaser.yml`, and tag-triggered `.github/workflows/release.yml` are present; the owned path was exercised and generic `gh release create` was not used | PASS |
| GitReins | Candidate tree has 0 tracked `.gitreins` paths. Live checkout has 236 untracked verdict files; freshest `c7695518` is bound to `742166fd`, not the candidate, and is internally inconsistent (`merge_recommendation=no_concerns_found` while top-level `passed=false`) | UNVERIFIED / blocker |
| Dogfood / QA | Latest durable dogfood is `docs/dogfood/2026-11-14-2243-dogfood.md` at `04e7bfc`, an ancestor with works-but-overselled findings, not a candidate-bound release battery. The latest QA annotation proves hosted CI `43700982860` green at `882edb0a` and native tests 22/22, but its act failure is the known missing docker CLI/daemon environment defect and does not clear candidate-bound clean-machine evidence | PARTIAL / blocker |
| Open release blockers | Existing rolling `RELEASE-013` remains pending/NO-GO. `QA-MISCHIEF-002-R2` is failed P1; `QA-MISCHIEF-CHAOS-4` is pending on a phantom scheduler target; `QA-MISCHIEF-FOREMAN-1` is in progress; `PROF-002` is review P0 and `PROF-003` is in progress P0. `INT-CI-10`, `DOC-29`, `MISCHIEF-DF-75`, `RELEASE-014`, and `RELEASE-015` remain open rows | BLOCKING |
| Deployed binary | `/home/kara/.local/bin/mischief` remains the stale `mischief v0.1.0` deployment (mtime 2026-10-16) | STALE / owner action |
| Version hygiene | No `CHANGELOG.md` or `Unreleased` heading; release contract is documented, but v0.2.0 prep must review release-facing claims before any tag | PREP needed |
| Authorization | No board row authorizes cutting `v0.2.0`; `RELEASE-013` explicitly remains NO-CUT. The scheduler template is not authorization | NO-CUT |

## Known issues

- `QA-MISCHIEF-002-R2` remains failed P1: the fresh JIT-agent TTL-owner recurrence is unresolved.
- `QA-MISCHIEF-CHAOS-4` remains pending on the nonexistent `local:/home/kara/mischief-chaos` / `/home/kara/wojons/mischief-chaos` target; it is a scheduler/config-owner defect.
- `QA-MISCHIEF-FOREMAN-1` remains in progress; act evidence is environment-limited and does not substitute for hosted exact-head release evidence.
- Candidate-bound GitReins is unavailable; the live untracked verdicts are not release evidence for `b0cac920d2208cba760edf392c7349a612a6703e`.
- `PROF-002`/`PROF-003` and additional PROF P1 rows remain unfinished product work.
- The deployed binary remains `v0.1.0`.
- `RELEASE-015` documents an incorrect snapshot stamp claim; the rehearsal itself passed.

## Unblock

1. Obtain a candidate-bound clean-machine QA/dogfood battery with the JIT-agent mechanism required by `QA-MISCHIEF-002-R2`, or record an explicit accepted release ruling.
2. Resolve or explicitly defer the unfinished P0/P1 product rows before publishing this product surface.
3. Correct the scheduler target owned by `QA-MISCHIEF-CHAOS-4` and prove a bare-dispatched tick resolves `/home/kara/mischief`.
4. Obtain candidate-bound GitReins evidence or an explicitly accepted equivalent gate.
5. Re-run releng at a stable candidate. Only after all gates and authorization are present should the foreman follow `docs/RELEASE.md`: annotated `v0.2.0` tag, tag CI, draft Release asset review, publish, and post-release verification.

No tag, GitHub Release, artifact publication, deployment mutation, or force-move was performed by this pass.

## Pass-8 closeout (same tick, re-entry)

This tick (`mischief-chaos-releng-2026-11-15-11-18-38`, fired 2026-11-15T11:18:38Z) had already written and pushed its pass-8 record — the artifact append in `bb3c0c1`, the rolling row refresh in `50d8d31`, and the row/artifact binding in `2b3aaa6` — before this closeout resumed the same pass. Nothing in the product tree changed in between, and no gate below is copied forward from that earlier phase without being re-read at the pushed tip.

**Carrier.** The pushed tip is `2b3aaa663c1bcbdfb9c6943f4de5d7966d44e792`. Its delta from pass-8's certified candidate `b0cac920d2208cba760edf392c7349a612a6703e` is exactly three paths — this artifact, `.coding-hermes/board/tasks.jsonl`, `.coding-hermes/board/events.jsonl` — i.e. **0 product-path changes**, so the earlier product measurements carry by construction rather than by assertion.

**Exact-tip CI re-read.** GitHub run `43709691390`, workflow `CI`, event `push`, `headSha=2b3aaa663c1bcbdfb9c6943f4de5d7966d44e792`, `completed/success`. Its single job `Build & Test (1.24)` ran 15 steps, 0 failed, including Build, Vet, Docs twins, Test, the selftest suite, the live N-012 battery cell, the per-arch shim landing proof, and both drift gates.

**Release surface re-read at the pushed tip.** `git ls-remote --tags origin` returns only the annotated `v0.1.0` (tag object `af9a38f`, peeled `b123b4e`); `gh release list` returns one published non-draft Release (`v0.1.0`, 2026-10-07); `git ls-remote --tags origin refs/tags/v0.2.0` returns 0 refs. The `v0.2.0` namespace is still free and nothing was published by any phase of this tick.

**Record correction.** The row's `detail.evaluated_at` was written as `2026-11-15T11:18:38-05:00`, which resolves to `16:18:38Z` — five hours *after* the tick actually fired (`11:18:38Z`). The tick id carries UTC. Corrected to `2026-11-15T11:18:38Z` in the same row.

**Blocker re-classification (new this closeout, and the one place the earlier phase was too coarse).** Three rows the earlier phase listed as open release-facing defects are *landed in the candidate tree* and are open only for evidence debt. Verified by reading the candidate's bytes, not the rows:

| Row | Claim in row | Measured in the candidate tree | Status |
|---|---|---|---|
| DOC-27 | documented `retention --dir` exits 2 | `docs/OPERATIONS.md` now reads `mischief retention --runs-dir <run dir>` (landed `812f2f9`) | fixed; row open for missing GitReins verdict |
| DOC-29 | documented `status --journal-dir` exits 2 | `docs/OPERATIONS.md:157` now reads `mischief status --dir <scratch>` (landed `a2ab3db`) | fixed; row open for missing GitReins verdict |
| MISCHIEF-DF-75 | `mischiefd_revert_total` omits `not_landed` | `internal/daemon/metrics.go` `knownOutcomes` now unions `reverter.DeclaredOutcomes()` (landed `742166f`) | fixed; row open on a tier-1 FAIL that the row itself attributes to ambient `GOTMPDIR`/symlink contamination |

The true product-open blocker set is therefore narrower than the open-row census: `PROF-002` (`review`, P0 — three-value outcome), `PROF-003` (`in_progress`, P0 — the `profile run` executor), `QA-MISCHIEF-002-R2` (`failed`, P1 — the JIT-agent TTL-owner recurrence), plus the two evidence gates that remain structurally absent: no candidate-bound clean-machine QA/dogfood battery and no candidate-bound GitReins verdict. `QA-MISCHIEF-CHAOS-4` is lane configuration (a nonexistent target path), not a product defect.

**Canonical checkout, flagged and not touched.** `/home/kara/mischief` is 4 commits behind the pushed tip and carries three uncommitted `.coding-hermes/board/tasks.jsonl` lines (the `wave-20261115-095307` worktree/branch/session bindings for `DOC-28` and `MISCHIEF-DF-76`) plus one untracked wave file. Those are the 09:53 foreman wave's bookkeeping, not this lane's, and a releng lane may not commit another lane's in-flight board write. Every gate read in this pass was taken from a clean detached worktree at the exact tip, so the measurements do not depend on that checkout; the divergence itself is noted for the wave's owner.

**Decision, unchanged: NO-CUT for `v0.2.0`.** The repository-owned contract is present and exercised, the exact-tip CI is green 15/15, and the release namespace is free — but two P0 product rows are unfinished and no candidate-bound QA or GitReins evidence exists. No tag, GitHub Release, artifact publication, deployment mutation, or force-move was performed by this pass.

## Pass-9 re-verification (same tick, carrier advance to `22484cf`)

`origin/main` advanced one commit past the pass-8 carrier: `2b3aaa6` → `22484cf251d06b340c310a3a93b39bef949efe12` (*"releng(mischief): pass-8 closeout — correct evaluated_at to the tick's UTC instant"*). This pass re-reads every gate at that tip rather than inheriting pass-8's readings.

**Carrier delta, measured.** `git diff --name-status 2b3aaa6 22484cf` = `.coding-hermes/audits/release/mischief-releng-2026-11-14-04-50-34-final.md` (M), `.coding-hermes/board/tasks.jsonl` (M) — **0 product paths**. Product-tree equality re-proved directly against the certified candidate: `git diff --name-only b0cac920d2208cba760edf392c7349a612a6703e 22484cf | grep -v '^.coding-hermes/'` → empty. Product measurements therefore carry by construction.

**Exact-tip CI.** Run `43711703567`, workflow `CI`, event `push`, `headSha=22484cf251d06b340c310a3a93b39bef949efe12`, `completed/success`, single job `Build & Test (1.24)`, 15 steps, **0 non-success steps**.

**Release surface at the tip.** `git ls-remote --tags origin` → only annotated `v0.1.0` (`git cat-file -t v0.1.0` = `tag`); `gh release list` → one published non-draft Release; `refs/tags/v0.2.0` absent. Nothing published by this pass; tag namespace still free.

**Local gates re-run at `22484cf`** (detached worktree, no product-file writes):

| Gate | Command | Result |
|---|---|---|
| build | `make build` | rc=0, no output |
| vet | `make vet` | rc=0, no output |
| test (authoritative) | `env -u GOTMPDIR make test` (TMPDIR = real dir) | rc=0, **22 packages ok, 0 FAIL** |
| release rehearsal | `make release-dry` | rc=0, `goreleaser check` → "1 configuration file(s) validated" |
| distribution | `rm -rf dist/release && make dist` | rc=0, amd64+arm64 tarballs + `checksums.txt` + `metadata.json` |

**Two host contaminations measured, both false reds — one already filed, one new.** The same tree that is 22/22 green above turns red purely from where the host's temp roots point. Both were reproduced both ways at this single commit:

1. `GOTMPDIR=/var/tmp/hermes-go-tmp` (a symlink to `-real`): `internal/chaos` fails `TestSymlinkEvidenceOK` (`lane_test.go:305`, resolved `…-real/TestSymlinkEvidenceOK…` vs declared primary `…/TestSymlinkEvidenceOK…`) and `TestVerifyAgainstMatches` (`lane_test.go:366`), and `cmd/mischief` fails `TestChaosCheckVerifiesInstantiatedLane` (`chaos_test.go:145`, check exit 2). With `env -u GOTMPDIR` all three pass. Already filed: **`RELEASE-014`**.
2. Stray `/tmp/catalog` (11 fault YAMLs, mtime 2026-10-19 22:52, owned by `kara`) — an ancestor of `/tmp`, so with `TMPDIR=/tmp` the repo-root walk-up in `internal/catalog` resolves outside the worktree: `TestLoadDefaultFindsRepoRootFromSubdir` fails `catalog_test.go:532: LoadDefault resolved outside the repo`. With `TMPDIR=/var/tmp/hermes-scratch` (no ancestor catalog) the same package is `ok`. Host residue, not a product defect — hosted CI cannot see it because ubuntu's `/tmp` has no catalog. Newly measured this pass; filed as **`RELEASE-016`**.

**Decision, unchanged: NO-CUT for `v0.2.0`.** Carrier is green and the release namespace is free, but the gate set is unchanged: `PROF-002` (`review`, P0), `PROF-003` (`in_progress`, P0), `QA-MISCHIEF-002-R2` (`failed`, P1) remain unfinished product work, and both candidate-bound evidence gates (clean-machine QA/dogfood battery, GitReins verdict) remain structurally absent. No tag, GitHub Release, artifact publication, deployment mutation, or force-move was performed by this pass.
