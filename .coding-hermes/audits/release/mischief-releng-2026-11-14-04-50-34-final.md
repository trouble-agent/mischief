# mischief release-readiness sweep — mischief-releng-2026-11-14-04-50-34

Target: trouble-agent/mischief
Canonical checkout: /home/kara/mischief
Forge/client: GitHub / gh
Certified candidate: 1126bcbea56cd86958816c148e828e09bae89181
Remote/default parity: local main == origin/main == origin/HEAD at 1126bcbea56cd86958816c148e828e09bae89181; ahead/behind 0/0.
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
