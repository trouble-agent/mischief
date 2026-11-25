# mischief docs gap audit — delivery 6 (re-fire, zero filings) — re-verification report

- tick: mischief-perf-2026-11-25-17-57-24 (mischief-docs lane); delivery 6
  (delivery 2 filed 4 P4 rows + published evidence artifact; delivery 3 re-verified and
  appended audit event 576; delivery 4 re-verified and died pre-report; delivery 5
  published delivery 4's transcript and its own recovery report)
- probe root: /home/kara/mischief (canonical checkout, clean); all commands run 2026-11-25
- audited tip: d2b795b2d3b540b51f6576b2c0eb8cf438733008

## Identity proofs (fresh this delivery)

```
$ git fetch origin main && git ls-remote origin refs/heads/main
d2b795b2d3b540b51f6576b2c0eb8cf438733008	refs/heads/main
$ git rev-parse HEAD origin/main          (both == d2b795b2d3b540b51f6576b2c0eb8cf438733008)
$ git rev-list --left-right --count origin/main...HEAD
0	0
$ git status --porcelain | wc -l
0
$ git diff --stat c551203..HEAD -- README.md CONTRIBUTING.md SECURITY.md docs skills .github
(empty) — reader corpora byte-identical from delivery 2's audited tip through HEAD;
delivery 1's census totals are inherited unchanged and no new reader-corpus finding
can exist in them
$ git diff --stat 62dd141..HEAD | tail -1
2 files changed, 227 insertions(+)   (audit evidence files only — no product or board change)
```

## Publication readback (from origin/main blobs, not the worktree)

Staged readback script (stand-in readback-d6-20261125.py, JSON-decoding every line):

- DOC-17-R2-R2, DOC-34-R2, DOC-11-R2, DOC-16-R2: all status=pending, priority=P4,
  title carries the P4 prefix, exactly one string field per row carries a trace-marker
  form (marker_forms=1 each, counted over every string field of the decoded row).
- Shipped extractor over the origin tasks blob
  (scripts/extract_row_markers.py --ids DOC-17-R2-R2,DOC-34-R2,DOC-11-R2,DOC-16-R2):
  rc=0, 4 rows printed, 4 marker-form lines total — exactly one complete marker per row,
  content matching delivery 2's commit message byte-for-byte.
- audit event id=576: event_type=audit, actor=mischief-docs, task_id=DOC-17-R2-R2,
  tick named in detail (events.jsonl origin: 661 lines, one match; no event added
  since — this delivery adds no board event because event 576 already names the tick
  and no board state changed).
- .coding-hermes/audits/mischief-docs-2026-11-25-17-57-24/audit-evidence.md read back
  from the origin blob: sha256 d063b9892992434c80b149afaa32f9f3e79b07e478bf2210a79e7851f58b6288
  (matches the digest recorded by deliveries 2, 3, 4 and 5).

## Premise re-probes (re-run fresh at d2b795b — all four hold)

A. env inventory gap (owned by DOC-17-R2-R2):
```
$ grep -rhoE '"(MISCHIEF[A-Z0-9_]*|MCFAULT[A-Z0-9_]*)"' --include='*.go' cmd internal | sort -u | wc -l
13
$ grep -c MISCHIEFD_TEST_OWNER_CHILD_DIR docs/reference/operator.md
0
$ sed -n '50p' README.md
| [docs/reference/operator.md](docs/reference/operator.md) | operator reference: every `MISCHIEF*`/`MCFAULT*` environment variable with its default, safe overri…
```
B. reverter journal schema undocumented (owned by DOC-34-R2):
```
$ grep -cE 'RecordType = "' internal/reverter/record.go
6
$ grep -rn 'reverter.jsonl' README.md CONTRIBUTING.md SECURITY.md docs/*.md docs/reference/*.md docs/prd/*.md | wc -l
0
$ grep -rc 'reverter.jsonl' skills/mischief-usage/SKILL.md
1
```
C. lane count rot (owned by DOC-11-R2):
```
$ sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT count(*) FROM projects WHERE name LIKE 'mischief%';"
11
$ sed -n '4p' docs/LANES.md
21 enabled rows shown below. The DB (`projects` table in
$ sed -n '52p' README.md
| [docs/LANES.md](docs/LANES.md) | lane doctrine: mischief's 21 scheduler rows — admission modes, cooldowns, workdirs — and why the schedule…
```
D. PRD verb table omissions (owned by DOC-16-R2):
```
$ grep -c 'mischief arm\|mischief land\|profile run\|profile list' docs/prd/mischief-v0.1.md
0
$ grep -n 'verb table in the PRD' README.md
146:it `no_op` is not implemented in v0.1 (see the verb table in the PRD). `plan`
```

## Board health and re-triage census

```
$ boardctl -C /home/kara/mischief validate   (tail)
RESULT: OK (267 warning(s))
```
267 warnings / 0 errors — byte-equal to the delivery 3/4/5 baseline; delta 0 (this
delivery changed no board files before its own evidence commit). The quoted warns are
pre-existing legacy event-id shapes.

Open P1-P3 rows on the origin board (policy census, ruling 2026-09-30; statuses
pending/in_progress/deferred/failed/blocked): 13 rows — INT-CI-10 P3, MISCHIEF-DF-76-R1
P3, NEVER-DONE P3 (perpetual fixture), PROF-012 P1, PROF-013 P2, PROF-020 P1, PROF-021
P2, QA-MISCHIEF-002-R2 P1, QA-MISCHIEF-008 P3, QA-MISCHIEF-CHAOS-4 P3,
QA-MISCHIEF-FOREMAN-2 P2 (failed), QA-MISCHIEF-FOREMAN-3 P3, RELEASE-013 P1. All
functional findings (release sweep, QA batteries, profiling, integration CI, perpetual
fixture) — none is documentation rot wearing a low severity, so there are no re-triage
targets this pass. docs/traceability-doctrine.md present at the tip (5150 bytes).

## Delivery trail (DuckBrain namespace mischief)

Prefix reads (both historical): /projects/mischief/docs-audit carries delivery-4 and
delivery-5 records (plus the 2026-11-18 audit record); /docs/ticks/<tick>/ carries
delivery-3. Delivery 5's promised continuation record landed (timestamp
2026-11-25T19:06:51Z) — no trail hole to disclose. This delivery writes one
continuation record at /projects/mischief/docs-audit/2026-11-25-175724-delivery6 after
its evidence commit lands.

## Conclusion

Nothing to file; nothing to amend; nothing to re-triage. filed_rows: 0, amended_rows: 0,
new_events: 0. The board already owns every premise this tick can produce; all four
re-verified at the published tip d2b795b.

Redaction note: this report is deliberately token-free of trace-marker forms; marker
presence is asserted via the extractor census above so no marker census over the
evidence tree counts this file as an authored marker.

STATUS: PARTIAL
TIER: T2 — premise re-probes are static source/docs greps; live legs this delivery:
scheduler DB read, board validate, origin blob reads and marker extractor; the findings'
own runtime tiers are inherited from delivery 2's committed artifact, not re-earned here
EVIDENCE: .coding-hermes/audits/mischief-docs-2026-11-25-17-57-24/audit-recheck-delivery6.md
(this file) + audit-evidence.md sha256 d063b9892992434c80b149afaa32f9f3e79b07e478bf2210a79e7851f58b6288
