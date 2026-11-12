# Release readiness evidence

Tick: `mischief-chaos-releng-2026-11-02-00-47-05`

| Section | Evidence |
|---|---|
| Target | `trouble-agent/mischief`; canonical repository remote verified from `origin`; candidate `main` SHA `4f55b176b5b11578c61ab6c4fb561970f2cde76e`; `HEAD`, `origin/main`, and `git ls-remote` agree. |
| Previous release | `v0.1.0` is the only remote semantic tag; the GitHub Release is published and non-draft with `checksums.txt`, Linux amd64, and Linux arm64 archives. |
| Range | `v0.1.0..HEAD` contains 500 commits. Conventional subjects: feat 0, fix 24, perf 0, chore 8, docs 31, refactor 0, test 1, other 436. No `BREAKING CHANGE:` footer or typed `!` marker. |
| Version decision | Conservative proposal: `v0.2.0` minor, because the range contains the user-facing arm command feature despite no `feat:` subject. No major signal. |
| Hosted CI | Exact candidate run `41372770720` completed successfully on the candidate SHA: Build & Test (1.24), build, vet, docs twins, tests, selftest, one live battery cell, per-architecture shim smoke, shim drift, and row-status drift all succeeded. Recent run history is green in the inspected window. |
| Release tooling | Repository-owned path verified: `docs/RELEASE.md`, `Makefile` targets `release-dry`, `dist`, and `tags`, `.goreleaser.yml`, and tag-triggered `.github/workflows/release.yml`. `make release-dry` passed GoReleaser configuration validation. No tag, Release object, or artifact mutation was attempted. |
| GitReins | UNVERIFIED/BLOCKED for this candidate: `gitreins guard --full --scope working-tree --json` cannot run because the candidate tree has no `.gitreins/config.yaml`; no candidate-bound verdict was found. Existing history is mixed and is not substituted for candidate-bound evidence. |
| Dogfood/QA | Latest dogfood remains `PROMISING-BUT-ROUGH`: local status/plan/arm/serve surfaces work, but fresh-machine install/selftest/battery evidence is skipped, load-gated, or harness-blocked. The fresh-agent TTL-owner path remains unresolved. |
| Active blockers | `QA-MISCHIEF-002-R2` is failed; `QA-MISCHIEF-007` is pending; `QA-MISCHIEF-CHAOS-1` is pending. The installed binary still reports `v0.1.0`. |
| Authorization | This scheduler readiness tick and the existing pending `RELEASE-013` readiness row do not authorize a tag or GitHub Release mutation. No explicit cut authorization naming `trouble-agent/mischief v0.2.0` was found. |
| Decision | `NO-GO`. Do not create or move `v0.2.0`. Refresh the existing `RELEASE-013` row with this candidate-bound evidence; do not create a duplicate release row. |
