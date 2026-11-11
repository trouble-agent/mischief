# mischief release-readiness sweep — mischief-releng-2026-11-11-22-37-08

Target: trouble-agent/mischief
Canonical checkout: /home/kara/mischief
Certified candidate: ae5b421fc407d2c7403b0fad75bae2cf71df6765
Baseline: v0.1.0, peeled commit b123b4e6dd2541b5b4d1ce89af42ef8922bae2bb

## Decision

NO-GO — do not tag or create a GitHub Release. No tag, Release object, or production artifact was mutated.

Semver proposal: v0.2.0 (minor). The v0.1.0..candidate range contains 520 commits (436 non-merge), with feat 1, fix 19, perf 0, chore 10, docs 31, refactor 0, test 1, 6 other conventional, and 368 project-tag-prefixed subjects (board 145, dogfood 70, foreman 18, qa 13, releng 11, wave 8, rest one-offs). Typed-bang subjects 0; BREAKING CHANGE footers 0; no major confirmation is required. The material feature is 0b52fb8, Prometheus metrics on a separately bound listener (MISCHIEF-OPS-001).

The cut is blocked on gate evidence, not on scope. CI is green on the exact candidate and the release tooling rehearses clean, but the two readiness gates that failed at the previous candidate are still unresolved and neither is re-proven at ae5b421.

## Evidence table

| Surface | Evidence | Result |
|---|---|---|
| Candidate parity | local main, origin/main, and `ls-remote refs/heads/main` all resolve to ae5b421fc407d2c7403b0fad75bae2cf71df6765; ahead/behind 0/0 | PASS |
| Candidate CI | GitHub CI run 43120322519, exact headSha ae5b421, event push, completed success; job 130858841316 Build & Test (1.24), 15 steps, 0 failed, 0 skipped — Build, Vet, Docs twins, Test, Selftest, N-012 battery cell, shim .so per-arch landing proof, shim drift gate, row-status drift gate all success | PASS |
| Recent CI history | `gh run list --repo trouble-agent/mischief --limit 20`: 43120322519, 43118780627, 43117650735, 43115954158, 43108683496, 43108414727 and older listed runs are completed success on their own SHAs | PASS for main; tag CI not applicable because no tag was cut |
| Repository release tooling | Repo owns the flow: docs/RELEASE.md, Makefile `release-dry`/`dist`/`tags`, .goreleaser.yml, and tag-triggered .github/workflows/release.yml that reruns the gates and publishes a DRAFT release for human review; fresh `make release-dry` at the candidate validates the goreleaser config (exit 0) | PASS for tooling rehearsal |
| Build rehearsal | `make dist` in a disposable candidate worktree at the earlier candidate produced linux amd64 and linux arm64 archives plus checksums.txt; snapshot output, not publishable assets | PASS |
| GitReins | No .gitreins/config.yaml; 0 of 220 stored verdict files contain the candidate SHA; newest stored history is 2026-10-31; census COMPLETE 119, INCOMPLETE 59, other 42 | UNVERIFIED / blocker |
| QA / clean machine | Latest battery (tick mischief-qa-2026-11-11-22-36-17) tested 069ebd5, not the certified candidate: OK 14, PASS 1, FAIL 1, UNVERIFIED 1, INFO 2, N/A 5. ci-pass and chaos-resource failed TestOwnerSurvivesKill9AndRevertsAtTTL (internal/reverter); chaos-disconnect cascade-unverified. QA-MISCHIEF-002-R2 remains status=failed at P1 with the agent-side mechanism unresolved | NO-GO / blocker |
| Previous release integrity | v0.1.0 asset mischief_v0.1.0_linux_amd64.tar.gz downloaded and extracted, `mischief version` prints mischief v0.1.0 (exit 0); `go build ./...` at v0.1.0 peel b123b4e exits 0 | PASS — refutes the open QA-MISCHIEF-FOREMAN-2 title premise that the previous release does not build |
| Deployed binary | /home/kara/.local/bin/mischief, mtime 2026-10-16 09:31, reports mischief v0.1.0 | STALE vs candidate |
| Release surface | v0.1.0 is the only remote tag and the only published non-draft Release (checksums.txt plus linux amd64/arm64 archives); remote v0.2.0 tag count 0; `gh release view v0.2.0` reports release not found | PASS — namespace clean for a v0.2.0 cut |

## Unblock

1. Resolve QA-MISCHIEF-002-R2 with a fresh JIT-agent battery run against ae5b421 that shows ci-pass and chaos-resource green on TestOwnerSurvivesKill9AndRevertsAtTTL, or identify and correct the agent-side mechanism.
2. Obtain candidate-bound GitReins evidence at ae5b421 (config plus verdict), or record an accepted project policy with an alternative exact-candidate gate.
3. Re-verify QA-MISCHIEF-FOREMAN-2's premise before dispatch: the v0.1.0 tag builds clean and the shipped v0.1.0 archive runs, so that row as titled is not reproducible on this host.
4. Cut only through docs/RELEASE.md (annotated tag push v0.2.0), then verify tag CI is green, the Release is complete with checksums plus both linux archives, the binary stamps v0.2.0, and the deployed binary is refreshed from that Release.

Correction: earlier same-tick evidence was bound to 069ebd5, then 2df89d9, then 73650da, then 236180e. The candidate advanced to ae5b421 when the PROF-001 merge landed (internal/backend_signal and internal/selftest). This record supersedes all earlier bindings for this tick; the board row and detail.json have been re-anchored to ae5b421.

No tag, Release object, artifact publication, or deployed binary was mutated by this sweep.
