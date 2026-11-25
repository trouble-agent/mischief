Mischief docs audit, tick mischief-perf-2026-11-25-17-57-24, delivery 7 (re-fire)
Audited origin/main and HEAD: 959f2339a6d1e1b8f58b98876d459cbe97facf54; fetch and ls-remote succeeded; divergence 0 0; clean checkout. `git diff --stat d2b795b..HEAD -- README.md CONTRIBUTING.md SECURITY.md docs skills .github` printed nothing. The earlier coverage census is inherited, not repeated.

Re-run probes and outputs:
`grep -rhoE '"(MISCHIEF[A-Z0-9_]*|MCFAULT[A-Z0-9_]*)"' --include='*.go' cmd internal | sort -u | wc -l` -> 13. `grep -c MISCHIEFD_TEST_OWNER_CHILD_DIR docs/reference/operator.md` -> 0. README.md:50 asserts every variable is covered. Existing owner DOC-17-R2-R2.
`grep -cE 'RecordType = "' internal/reverter/record.go` -> 6. `grep -rn 'reverter.jsonl' README.md CONTRIBUTING.md SECURITY.md docs/*.md docs/reference/*.md docs/prd/*.md | wc -l` -> 0; `grep -rc 'reverter.jsonl' skills/mischief-usage/SKILL.md` -> 1. Existing owner DOC-34-R2; skills are a separate audience/corpus.
`sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT count(*) FROM projects WHERE name LIKE 'mischief%';"` -> 11; docs/LANES.md:4 and README.md:52 say 21. Existing owner DOC-11-R2. The query counts matching project names, not necessarily enabled rows; investigate classification before asserting enabled count.
`grep -c 'mischief arm\|mischief land\|profile run\|profile list' docs/prd/mischief-v0.1.md` -> 0; README.md:146 points to the PRD verb table. Existing owner DOC-16-R2.

Origin tasks readback: all four existing owners pending P4 with P4 in title and one marker-bearing field apiece. Origin audit event 576 occurs once. `git show origin/main:.coding-hermes/audits/mischief-docs-2026-11-25-17-57-24/audit-evidence.md | sha256sum` -> d063b9892992434c80b149afaa32f9f3e79b07e478bf2210a79e7851f58b6288, matching delivery 6's cited value. `boardctl -C /home/kara/mischief validate` -> exit 0, RESULT: OK (267 warning(s)). Zero rows created or amended; no event appended.

Limits: this delivery ran static premise probes, not the individual executable paths; runtime proof is inherited, not repeated. We did not re-run the full surface census, priority re-triage or live daemon checks. This report is stand-in only and not published.
