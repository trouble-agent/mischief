# mischief release-readiness sweep

Tick: mischief-releng-2026-11-27-10-14-45
Pass: 1
Measured at: 2026-11-27T10:42:34Z
Target: trouble-agent/mischief
Canonical checkout: /home/kara/mischief
Release remote: origin git@github.com:trouble-agent/mischief.git
Forge/client: GitHub / gh
Default branch: main

## Decision

NO-CUT. Proposed version: v0.2.0 (minor). No tag, GitHub Release, publication, deployment mutation, or force-move was performed.

The candidate is 6d6d72328af011fb22e469fd9652ca301f25c7b6 and equals origin/main. The v0.1.0 remote tag peels to b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb. The remote has no v0.2.0 tag and GitHub has no v0.2.0 Release.

## Evidence table

id | state | value
candidate | VERIFIED | 6d6d72328af011fb22e469fd9652ca301f25c7b6 == origin/main
baseline | VERIFIED | v0.1.0 peeled commit b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb
census | VERIFIED | 776 total commits, 127 merges, 649 non-merges in v0.1.0..HEAD
semver | PROPOSED | 1 feat, 22 fix, 0 typed-bang subjects, 0 BREAKING CHANGE footers -> v0.2.0 minor
ci | GREEN | GitHub CI run 45957796370, exact head 6d6d72328af011fb22e469fd9652ca301f25c7b6, completed success; check-run 140368216270 success; 17/17 steps successful
local-build | PASS | go build ./... in detached worktree at candidate
local-vet | PASS | go vet ./... in detached worktree at candidate
local-tests | PASS | env -u GOTMPDIR TMPDIR=/home/kara/.hermes/cache/scratch/mischief-tmp-20261127-101445 go test -count=1 ./..., all packages passed
release-config | PASS | make release-dry; GoReleaser config validated
snapshot-dist | PASS | make dist; linux amd64 and arm64 archives plus checksums built; snapshot version 0.0.0-SNAPSHOT-6d6d723
release-surface | VERIFIED | repo-owned docs/RELEASE.md, Makefile, .goreleaser.yml, and tag-triggered .github/workflows/release.yml; tag push is the publication trigger and workflow creates a draft Release
registry | N/A | no npm, pyproject.toml, or Dockerfile/native registry channel detected
candidate-gitreins | UNVERIFIED | no tracked .gitreins configuration; no verdict names candidate 6d6d72328af011fb22e469fd9652ca301f25c7b6; newest local verdicts are ancestor-bound
candidate-dogfood | UNVERIFIED | newest durable dogfood artifact docs/dogfood/2026-11-14-2243-dogfood.md is bound to 04e7bfc, not candidate
board | GREEN_WITH_WARNINGS | boardctl validate: RESULT OK (267 pre-existing warnings); 276 rows: 261 complete, 2 failed, 13 pending
release-row | OPEN | RELEASE-013 is the rolling NO-CUT row; QA-MISCHIEF-002-R2 remains pending P1

## Commit census

Range is literal v0.1.0..6d6d72328af011fb22e469fd9652ca301f25c7b6, with merges included in the total and excluded from the conventional-prefix bucket. Non-merge buckets reconcile to 649: feat 1, fix 22, perf 0, chore 16, docs 54, refactor 0, test 4, ci 1, other conventional 506, non-conventional 45. Merge count is 127; total count is 776. Complete-message footer inspection found 0 BREAKING CHANGE/BREAKING-CHANGE footers and the subject scan found 0 typed-bang subjects.

The product tree changed materially since the prior certified candidate: the current tip includes profile implementation and tests, catalog profiles, CI/release/doc updates, and related product files. Prior candidate evidence is not silently carried.

## Release tooling and publication order

The repository-owned path is mandatory. docs/RELEASE.md specifies an annotated v* tag pushed to origin; .github/workflows/release.yml then reruns build, vet, uncached internal tests, selftest, one battery cell, per-architecture shim smoke proof, drift gate, and GoReleaser. .goreleaser.yml creates a DRAFT GitHub Release. Do not run gh release create beside this workflow; it would bypass or race the repository-owned publisher. The local snapshot dry-run produced archives named mischief_0.0.0-SNAPSHOT-6d6d723_linux_{amd64,arm64}.tar.gz and checksums.txt. No real release artifact was published.

## Known issues

QA-MISCHIEF-002-R2 remains an open P1 release gate: its JIT-agent TTL-owner recurrence is still pending and candidate-bound clean-machine evidence is absent. Workaround: do not cut v0.2.0; re-run the named fresh-agent battery and bind its result to the candidate before authorization.

Candidate-bound GitReins is unavailable because the repository has no tracked .gitreins configuration; ancestor verdicts do not certify this candidate. Tracking: RELEASE-013.

Candidate-bound dogfood is unavailable. The latest durable dogfood report is pre-candidate and reports works-with-findings plus an unattributed act Test failure. Tracking: RELEASE-013.

The installed /home/kara/.local/bin/mischief is a stale v0.1.0 developer artifact: vcs.revision=b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb and vcs.time=2026-10-07T16:28:54Z. This is not proof of a deployed service; no mischief systemd unit was found. Tracking: RELEASE-013.

## Open blockers and unblock path

1. QA-MISCHIEF-002-R2: obtain fresh JIT-agent battery evidence on the candidate and close the row through the board/GitReins close order.
2. RELEASE-013: refresh the rolling release row with this candidate and exact receipts; retain NO-CUT until all named gates resolve.
3. Candidate-bound GitReins: establish the repository's configured verdict path or explicitly record the owner-approved substitution; ancestor PASS results are not exact-candidate evidence.
4. Candidate-bound dogfood/install: run and persist the battery against the candidate or a published artifact from that candidate.
5. Owner authorization: a minor cut remains lane-authorized only after all gates are green; no release authorization was present in this pass.

## What was not mutated

No source files, version surfaces, changelog, tag, GitHub Release, registry, Docker image, deployment, or force-moved ref was changed. This artifact and the rolling board row are the only intended durable release-readiness changes for this pass.
