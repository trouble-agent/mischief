# Public-readiness retrospective audit — MSF-019

Date: 2026-10-06 · Auditor: MSF-019 worker (retrospective pass) · Repo: `trouble-agent/mischief` (public since the 2026-10-02 flip)

Note on wording: this report deliberately does not re-quote the literal
private tokens it found (same doctrine as the pre-flip scrub — the evidence
strings themselves stay out of the tree). Each finding names its class,
location, hit count and replacement instead; the exact commands to re-derive
every count are given, so any reader can reproduce the numbers.

The visibility flip already happened; commit 5e0fafc (2026-10-01) scrubbed
`.coding-hermes/board/fixtures.jsonl`, `.coding-hermes/board/tasks.jsonl` and
`.gitignore` of absolute home paths, fleet config names, the knowledge-store
name and internal gap ids — content only, same row order. This audit is the
retrospective: what the flip and that scrub LEFT UNCOVERED, in the tree and
in git history, with severity judged honestly (a public repo with restricted
push access is exposed mainly to scraped secrets, so credential-shaped
strings were checked first and are the top finding).

## 1. Top finding: no real secrets anywhere in history (NEGATIVE, verified)

A credential-shape sweep over the full history patch (`git log -p --all`,
4.56 MB, all 156 commits) and over the HEAD tree:

| shape | history hits | disposition |
|---|---|---|
| `sk-…` (20+ chars) | 4 | synthetic — the repo's own redaction-test fixtures (elided: `sk-abc…`), added by MSF-020's scrub-verification tests |
| `ghp_…` | 2 | synthetic test fixture (elided) |
| `AKIA…` (AWS) | 1 | synthetic placeholder (the suffix spells "EXAMPLE") |
| `xox[baprs]-` | 1 | the scrubber's own prefix-matching table (test data) |
| `api_key=` / `password=` assignments | 13 | all inside `internal/journal/(journal\|scrub)_test.go` fixtures with explicit `gitleaks:allow` comments; the assigned values are invented placeholders, not credentials |
| `BEGIN … PRIVATE KEY` | 0 | — |
| 64-hex runs | 450 | board row fingerprints, commit SHAs, agent ids — content hashes, not credentials |

The `.gitleaks.toml` allowlist (045fc3f) scopes exactly these test files.
At HEAD the same shapes appear only as those synthetic fixtures.
**No rotation is warranted; no secret reached the public repo.**

## 2. Git history carries the private identifiers (real, low severity, no rewrite required)

Quantified per pattern (pickaxe `-S` commits that ADD/REMOVE the string; tree
grep = total lines across every revision):

| pattern (described) | commits | tree-hits across history | nature |
|---|---|---|---|
| absolute `/home/<user>` paths | 60 | 4,480 | operator home dir + worktree paths in wave manifests, board rows, commit messages |
| the fleet gate's name (`gitreins`) | 57 | (gate name) | verdict/gate prose in board closeouts |
| the worker model lane (`zai-glm-default`) | 14 | 26 | worker dispatch lane label |
| two private sibling project names | 5 | (incl. name globs in one incident note) | private project names |
| an internal harness lane name | 3 | (QA case-file label) | internal harness name |
| an internal memory-daemon service name | 1 | 1 (code comment) | internal service name |

Git metadata is clean of people: authors/committers are only
`totalwindupflightsystems@gmail.com` (114) and the gate's
`gitreins@localhost` (42). The replay journal was never committed:
0 tracked files match; `.journals/`, `runs/`, `*.journal.jsonl` are
gitignored. No `.env`/key/pem/token files are tracked.

**Severity:** low. What leaks is topology and tooling names — where the
operator's home is, that a scheduler/agent harness drives the repo, what the
worker model lane is called. No credentials, no customer data, no security
boundary is weakened (the repo's threat surface — fault injection — is fully
documented by design). **Recommendation: NO history rewrite.** A rewrite
would invalidate every SHA that board rows and `ch:trace` trailers cite,
for a class of exposure that does not include a single secret. If the owner
later wants history cleaned anyway, the safe order is: freeze pushes →
`git filter-repo --replace-text` with the §3 table → force-push →
re-clone all worktrees → re-verify the §5 gates.

## 3. Tree residuals found by this audit — ALL FIXED in this branch

The scrub predated the post-flip wave commits: board rows and wave manifests
written AFTER 5e0fafc re-introduced the same classes. Found at audit start,
fixed here (counts are line hits at audit start → after this branch):

| class | files | before → after | replacement |
|---|---|---|---|
| `/home/<user>/...` absolute paths | 14 (board, events, 9 wave manifests) | 37 → 0 | `~/worktrees/...`, `~/mischief` |
| the owner's first name in board rulings + one code comment | 2 | 10 → 0 | "owner ruling 2026-09-30" / "the null-with-reason rule" |
| two private sibling project names (name globs) | 1 (board incident note) | 2 → 0 | `project-*` / `otherproject-*` |
| internal memory-daemon service name | 1 code comment | 1 → 0 | "the host's memory daemon (internal service)" |
| internal harness lane name | 3 files | 4 → 0 | "QA family (QA-19/20)" |
| harness-script paths under the agent's home config dir | 8 files (docs ×6, code comments ×3, board ×2) | 12 → 0 | `~/…/bunker-qa.sh` |
| a personal host name in test fixtures | 2 (paired producer/assertion) | 2 → 0 | `devHostA` |
| `/home/<user>` in test fixture strings | 2 | 2 → 0 | `/home/demo` |

Remaining, deliberate, non-sensitive (disclosed, not redacted):

- `.coding-hermes/` directory name + `coding-hermes` namespace string (21
  lines) and `gitreins` gate name (15 lines): these are the open-source
  fleet tooling's own names; the board path is referenced by in-repo
  `ch:trace` trailers and the SKILL.md onboarding doc. Renaming them is a
  product decision, not a leak.
- `glm-5.3-flash` / `zai-glm-default` (26 lines): worker dispatch metadata in
  historical wave records; identity-neutral model/lane labels.
- `trouble-hub` / `hermes-gateway` in rehearsal YAML selectors: the public
  sibling's stack and the local agent runtime, used exactly as the
  rehearsal-contract intends (placeholder targets that never resolve).
- `/home/bunker-$AGENT_ID` in `scripts/bunker-plane.sh`: a computed remote
  path pattern, not a host path. `/home/<user>` in the board AC text: the
  audit criterion's own wording.

## 4. Incident text in docs/ (audited, kept — with names redacted)

The PRD case files name real past incidents by internal QA/Trouble ids
(QA-TROUBLE-2/3/6/8, TRBL-031/040/041) and replay experiments encode the
2026-08-29/30 archive hole (ex-004). This is intentional design material —
the project's founding evidence — and reads as such to an outside auditor;
the private INFRASTRUCTURE names riding inside those stories are what got
redacted (§3). `docs/dogfood/*.md` internal-audit reports kept; the two
fleet-process name references in them were genericized.

## 5. Sibling contract + live visibility (acceptance)

| criterion | result |
|---|---|
| LICENSE | present, MIT, "Copyright (c) 2026 trouble-agent" ✓ |
| README.md | present, front door, no private paths ✓ |
| SECURITY.md | WAS MISSING (sibling `trouble` has one) → added this branch: advisory link, scope notes (rehearsal files are not attack surface; sanction-gate bypass = high severity), supported line ✓ |
| CONTRIBUTING.md | WAS MISSING → added this branch: spec-first rule, safety non-negotiables, build/test/docs-twin commands, PR expectations ✓ |
| unauthenticated GET `https://github.com/trouble-agent/mischief` | HTTP 200 (anonymous, verified live) ✓ |
| unauthenticated raw README | HTTP 200 ✓ |
| `git grep -c /home/<user>` at HEAD | 0 files ✓ |
| `git grep` for every fixed class in §3 | 0 ✓ |

## 6. Gates run on this branch

- `go build ./...`, `go vet ./...` — clean
- `go test -count=1 ./...` — 20/20 packages ok (incl. the edited
  doctor/plane/battery/backend_resource/rails suites; the doctor pair
  producer/assertion was renamed consistently)
- `python3 tools/gen-docs.py --check` — 0 stale twins (PRD + EXPERIMENTS
  HTML regenerated after their .md edits)
- board JSONL re-validated line-wise JSON after every edit pass; row count
  and fingerprint census unchanged

## 7. Residual risks & recommendations

1. **Re-leak mechanism (the root cause of §3):** `.coding-hermes/board/` is
   tracked, and every foreman close-out appends fresh rows that quote
   worktree paths and harness names. This audit redacted the current rows;
   the next close-out can re-leak. Recommendation: a CI grep gate (same
   pattern set as §3) on `.coding-hermes/**`, or untracking the board
   (owner decision — the board doubles as public evidence of process).
2. **History rewrite: not recommended** (§2). Revisit only if the owner
   judges the topology disclosure itself unacceptable.
3. **SECURITY.md email route** points at the sibling's advisory flow;
   replace with a mischief-specific contact when one exists.

---
ch:trace row=MSF-019 evidence=docs/public-readiness-audit-MSF-019.md witness=live:curl http200
