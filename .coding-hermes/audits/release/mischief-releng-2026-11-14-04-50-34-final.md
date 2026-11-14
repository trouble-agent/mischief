# mischief release-readiness sweep — mischief-releng-2026-11-14-04-50-34

Target: trouble-agent/mischief
Canonical checkout: /home/kara/mischief
Forge/client: GitHub / gh
Certified candidate: d6d605929a50071524b543016e7dc8a9052a1439
Remote/default parity: local main == origin/main == origin/HEAD at d6d605929a50071524b543016e7dc8a9052a1439; ahead/behind 0/0.
Baseline: v0.1.0, peeled commit b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb.

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
