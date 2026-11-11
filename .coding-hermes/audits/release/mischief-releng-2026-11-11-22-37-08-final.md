# mischief release-readiness sweep — mischief-releng-2026-11-11-22-37-08

Target: trouble-agent/mischief
Canonical checkout: /home/kara/mischief
Certified candidate: 73650dada8f045e0f2a5f040e503054615e9b9ee
Baseline: v0.1.0, peeled commit b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb

## Decision

NO-GO — do not tag or create a GitHub Release. No tag, Release object, or production artifact was mutated.

Semver proposal: v0.2.0 (minor). The v0.1.0..candidate range contains 516 commits (433 non-merge), with feat 1, fix 19, perf 0, chore 10, docs 31, refactor 0, test 1, other 371, typed-bang 0, and BREAKING CHANGE footer 0. The material feature is 0b52fb8, Prometheus metrics on a separately bound listener.

## Evidence table

| Surface | Evidence | Result |
|---|---|---|
| Candidate CI | Run 43117650735, exact head 73650da, completed success; check job 130849679727 Build & Test (1.24) success | PASS |
| Recent CI | gh run list --repo trouble-agent/mischief --limit 20: latest runs 43117650735, 43115954158, 43108683496, 43108414727, 42593047221, 42419731559, 41900893513, 41821669599, and 41732617197 were successful on their own SHAs | PASS for main; tag CI not applicable because no tag was cut |
| Release tooling | docs/RELEASE.md, Makefile release-dry/dist/tags, .goreleaser.yml, and tag-triggered .github/workflows/release.yml exist; fresh `make release-dry` passed | PASS for tooling rehearsal |
| GitReins | No .gitreins/config.yaml; 0 stored verdicts contain the candidate full SHA; stored history is mixed: 151 passed, 69 failed/incomplete | UNVERIFIED / blocker |
| Dogfood / QA | Latest clean-machine evidence recorded in 73650da: ci-pass and chaos-resource failed on TestOwnerSurvivesKill9AndRevertsAtTTL; fresh-install and upgrade passed; chaos-disconnect was cascade-unverified. QA-MISCHIEF-002-R2 remains the open failed recurrence with mechanism unresolved | NO-GO |
| Deployed binary | /home/kara/.local/bin/mischief, mtime 2026-10-16, reports mischief v0.1.0 | STALE |
| Existing release | v0.1.0 is the only remote tag and published non-draft release with checksums.txt and linux amd64/arm64 archives | PASS |
| v0.2.0 namespace | Remote tag absent; `gh release view v0.2.0` reports release not found | PASS |

## Unblock

1. Resolve QA-MISCHIEF-002-R2 with a fresh JIT-agent battery showing ci-pass and chaos-resource green, or identify/correct the agent-side mechanism.
2. Obtain candidate-bound GitReins evidence, or record an accepted project policy and alternative exact-candidate gate.
3. After any release-surface change, obtain fresh exact-head CI and cut only through docs/RELEASE.md; verify tag CI, Release assets/checksums, binary v0.2.0 stamp, and tag-SHA CI.

Correction: earlier same-tick evidence was bound to 069ebd5 and then 2df89d9. The candidate advanced to 73650da with new QA evidence; this record supersedes those bindings.

No tag, Release object, artifact publication, or deployed binary was mutated by this sweep.
