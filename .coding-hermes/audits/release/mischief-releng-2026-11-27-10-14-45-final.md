# mischief release-readiness sweep

Tick: mischief-releng-2026-11-27-10-14-45
Pass: 2
Measured at: 2026-11-27T11:10:38Z
Target: trouble-agent/mischief
Canonical checkout: /home/kara/mischief
Release remote: origin git@github.com:trouble-agent/mischief.git
Forge/client: GitHub / gh
Default branch: main

## Decision

NO-CUT. Proposed version: v0.2.0 (minor). No tag, GitHub Release, publication, deployment mutation, or force-move was performed.

The candidate is 5497e244c6fbff5ae46ae0935b5f95fd91397330 and equals origin/main. The v0.1.0 remote tag is annotated and peels to b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb. The remote has no v0.2.0 tag and GitHub has no v0.2.0 Release.

## Evidence table

id | state | value
candidate | VERIFIED | 5497e244c6fbff5ae46ae0935b5f95fd91397330 == origin/main; worktree clean
baseline | VERIFIED | v0.1.0 annotated, peeled commit b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb
census | VERIFIED | 778 total commits, 127 merges, 651 non-merges in v0.1.0..HEAD
semver | PROPOSED | 1 feat, 22 fix, 0 typed-bang subjects, 0 BREAKING CHANGE footers -> v0.2.0 minor
ci | GREEN | GitHub CI run 45985692032, exact head 5497e244c6fbff5ae46ae0935b5f95fd91397330, completed success; Build & Test (1.24) success; 16 steps, 0 failed
local-build | PASS | go build ./... in detached worktree at candidate
local-vet | PASS | go vet ./... in detached worktree at candidate
local-tests | PASS | go test -count=1 ./..., all 22 package results passed with a real TMPDIR/GOTMPDIR in a clean detached worktree
release-config | PASS | make release-dry; GoReleaser configuration validated
snapshot-dist | PASS | make dist; linux amd64 and arm64 archives plus checksums built; snapshot version 0.0.0-SNAPSHOT-5497e24
release-surface | VERIFIED | repo-owned docs/RELEASE.md, Makefile, .goreleaser.yml, and tag-triggered .github/workflows/release.yml; tag push is the publication trigger and workflow creates a draft Release
registry | N/A | no npm, pyproject.toml, or Dockerfile/native registry channel detected
candidate-gitreins | UNVERIFIED | no tracked .gitreins configuration and no verdict is bound to candidate 5497e244c6fbff5ae46ae0935b5f95fd91397330; 2026-11-27 local verdicts are ancestor-bound
candidate-dogfood | UNVERIFIED | newest durable dogfood artifact docs/dogfood/2026-11-14-2243-dogfood.md is bound to 04e7bfc, not candidate
board | GREEN_WITH_WARNINGS | boardctl validate: RESULT OK (267 pre-existing warnings); RELEASE-013 is the rolling pending NO-CUT row
release-row | OPEN | RELEASE-013 remains pending P1; QA-MISCHIEF-002-R2 remains pending P1
authorization | BLOCKED | no explicit owner cut authorization is present in the row/thread evidence

## Commit census

Range is literal v0.1.0..5497e244c6fbff5ae46ae0935b5f95fd91397330, with merges included in the total and excluded from the conventional-prefix bucket. Non-merge buckets reconcile to 651: feat 1, fix 22, perf 0, chore 16, docs 54, refactor 0, test 4, ci 1, other conventional 508, non-conventional 45. Merge count is 127; total count is 778. Complete-message footer inspection found 0 BREAKING CHANGE/BREAKING-CHANGE footers and the subject scan found 0 typed-bang subjects.

The current tip includes profile implementation and tests, catalog profiles, CI/release/doc updates, and related product files. Prior candidate evidence is not silently carried. The old RELEASE-017 premise-gap finding is stale: `catalog/profiles/` and the profile CLI are present in this tree, so it is not a current release blocker.

## Release tooling and publication order

The repository-owned path is mandatory. docs/RELEASE.md specifies an annotated v* tag pushed to origin; .github/workflows/release.yml then reruns build, vet, uncached internal tests, selftest, one battery cell, per-architecture shim smoke proof, drift gate, and GoReleaser. .goreleaser.yml creates a DRAFT GitHub Release. Do not run gh release create beside this workflow; it would bypass or race the repository-owned publisher. The local snapshot dry-run produced archives named mischief_0.0.0-SNAPSHOT-5497e24_linux_{amd64,arm64}.tar.gz and checksums.txt. No real release artifact was published.

## Known issues

QA-MISCHIEF-002-R2 remains an open P1 release gate: its JIT-agent TTL-owner recurrence is still pending and candidate-bound clean-machine evidence is absent. Workaround: do not cut v0.2.0; re-run the named fresh-agent battery and bind its result to the candidate before authorization.

Candidate-bound GitReins is unavailable because the repository has no tracked .gitreins configuration; ancestor verdicts do not certify this candidate. Tracking: RELEASE-013.

Candidate-bound dogfood is unavailable. The latest durable dogfood report is pre-candidate and reports WORKS-BUT-OVERSELLED with documented-command and output/reporting findings plus an unattributed act failure. Tracking: RELEASE-013.

The installed /home/kara/.local/bin/mischief is stale v0.1.0, reports the v0.1.0 revision, and has mtime 2026-10-16. No mischief systemd unit was found; this is stale local-binary evidence, not proof of a deployed service. Tracking: RELEASE-013.

The current tip contains the previously missing `catalog/profiles/` tree and profile CLI implementation; RELEASE-017's premise-gap finding is stale and is not a current blocker.

## Open blockers and unblock path

1. QA-MISCHIEF-002-R2: obtain fresh JIT-agent battery evidence on the candidate and close the row through the board/GitReins close order.
2. RELEASE-013: refresh the rolling release row with this candidate and exact receipts; retain NO-CUT until all named gates resolve.
3. Candidate-bound GitReins: establish the repository's configured verdict path or explicitly record the owner-approved substitution; ancestor PASS results are not exact-candidate evidence.
4. Candidate-bound dogfood/install: run and persist the battery against the candidate or a published artifact from that candidate.
5. Owner authorization: a cut remains owner-authorized only after all gates are green; no release authorization was present in this pass.

## What was not mutated

No source files, version surfaces, changelog, tag, GitHub Release, registry, Docker image, deployment, or force-moved ref was changed. This artifact and the rolling board row are the only intended durable release-readiness changes for this pass.
