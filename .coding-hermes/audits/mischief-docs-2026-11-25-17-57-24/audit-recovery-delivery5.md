# mischief docs gap audit — delivery 5 (recovery of delivery 4) — report

- tick: mischief-perf-2026-11-25-17-57-24 (mischief-docs lane); delivery 5
- delivery 2 (6790664) filed the tick's 4 P4 rows; delivery 3 (62dd141) re-verified and
  appended audit event 576; delivery 4 completed the same re-verification at the same tip,
  wrote its DuckBrain record (2026-11-25T18:49Z), and died before delivering its chat
  report — it committed nothing to the repo. This delivery publishes delivery 4's probe
  transcript durably under the repo evidence convention and records its own fresh
  re-verification. filed_rows: 0, amended_rows: 0, new_events: 0 (event 576 already names
  this tick; a second audit event would duplicate it).
- audited tip: 62dd14102cbcc303c28e8a38e0a9c41014773f03 (reader corpora and board inputs
  byte-identical to delivery 2's audited state at c551203 — proof below).

## Identity proofs (fresh this delivery)

```
$ git fetch origin main && git ls-remote origin refs/heads/main
62dd14102cbcc303c28e8a38e0a9c41014773f03	refs/heads/main
$ git rev-list --left-right --count origin/main...HEAD
0	0
$ git status --porcelain
(empty)
$ git diff --stat c551203 62dd141 -- README.md CONTRIBUTING.md SECURITY.md docs skills .github
(empty) — reader corpora byte-identical to delivery 2's audited tip; its census totals inherited
```

## Publication readback (from origin/main blobs, not the worktree)

- DOC-17-R2-R2, DOC-34-R2, DOC-11-R2, DOC-16-R2: status=pending, priority=P4, titles carry
  the P4 prefix and the doc-severity policy note (ruling 2026-09-30).
- extract_row_markers.py --ids DOC-17-R2-R2,DOC-34-R2,DOC-11-R2,DOC-16-R2 over the origin
  tasks blob: 4 rows printed, exactly one complete marker per row, rc=0.
- audit event id=576: event_type=audit, actor=mischief-docs, task_id=DOC-17-R2-R2
  (present in origin events.jsonl, 661 lines).
- .coding-hermes/audits/mischief-docs-2026-11-25-17-57-24/audit-evidence.md read back from
  the origin blob: sha256 d063b9892992434c80b149afaa32f9f3e79b07e478bf2210a79e7851f58b6288
  (matches the digest recorded by deliveries 2, 3 and 4).

## Premise re-probes (re-run fresh at 62dd141 — all four hold)

A. env inventory gap (owned by DOC-17-R2-R2):
```
$ grep -rhoE '"(MISCHIEF[A-Z0-9_]*|MCFAULT[A-Z0-9_]*)"' --include='*.go' cmd internal | sort -u | wc -l
13
$ grep -c MISCHIEFD_TEST_OWNER_CHILD_DIR docs/reference/operator.md
0
$ sed -n '50p' README.md
| [docs/reference/operator.md](docs/reference/operator.md) | operator reference: every `MISCHIEF*`/`MCFAULT*` environment variable with its default, safe override values, supported-knob vs internal-only status, and troubleshooting notes |
```

B. reverter journal schema undocumented (owned by DOC-34-R2):
```
$ grep -cE 'RecordType = "' internal/reverter/record.go
6
$ grep -rc 'reverter.jsonl' skills/mischief-usage/SKILL.md
1
$ grep -rn 'reverter.jsonl' README.md CONTRIBUTING.md SECURITY.md docs/*.md docs/reference/*.md docs/prd/*.md | wc -l
0
```

C. lane count rot (owned by DOC-11-R2):
```
$ sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT count(*) FROM projects WHERE name LIKE 'mischief%';"
11
$ sed -n '4p' docs/LANES.md
21 enabled rows shown below. The DB (`projects` table in
$ sed -n '52p' README.md
| [docs/LANES.md](docs/LANES.md) | lane doctrine: mischief's 21 scheduler rows — admission modes, cooldowns, workdirs — and why the scheduler DB (not `fleet.toml`) is the source of truth |
```

D. PRD verb table omissions (owned by DOC-16-R2):
```
$ grep -c 'mischief arm\|mischief land\|profile run\|profile list' docs/prd/mischief-v0.1.md
0
$ grep -n 'verb table in the PRD' README.md
146: it `no_op` is not implemented in v0.1 (see the verb table in the PRD). `plan`
```

## Board health and re-triage census

```
$ boardctl -C /home/kara/mischief validate   (tail)
RESULT: OK (267 warning(s))
```
267 warnings / 0 errors — byte-equal to the delivery 3/4 baseline; delta 0 (this delivery
changed no board files). The quoted warn lines are pre-existing legacy event-id shapes.

Open P1-P3 rows on the origin board (policy census, ruling 2026-09-30): NEVER-DONE P3,
QA-MISCHIEF-002-R2 P1, QA-MISCHIEF-008 P3, RELEASE-013 P1, PROF-012 P1, PROF-013 P2,
PROF-020 P1, PROF-021 P2, QA-MISCHIEF-FOREMAN-2 P2 (status failed), QA-MISCHIEF-FOREMAN-3
P3, INT-CI-10 P3, QA-MISCHIEF-CHAOS-4 P3, MISCHIEF-DF-76-R1 P3 (RELEASE-010/011 and
QA-MISCHIEF-CHAOS-2/3 carry P1-P3 fields but are closed). All functional findings
(release sweeps, QA batteries, profiling, integration CI, dogfood config, perpetual
fixture) — none is documentation rot wearing a low severity, so there are no re-triage
targets this pass. docs/traceability-doctrine.md present at the tip (5150 bytes).

## Delivery trail (DuckBrain namespace mischief, domain event)

- /docs/ticks/mischief-perf-2026-11-25-17-57-24/delivery-3 — delivery 3's record
  (commit 62dd141, event 576, the four rows).
- /projects/mischief/docs-audit/2026-11-25-175724-delivery4 — delivery 4's record; its
  probe transcript sha256 0a685cf0c139f5cfa8a0cb67670d262f3cda653e47d2694c552538fd0687e648
  re-verified byte-for-byte against the stand-in file, which is now published beside this
  report (digest unchanged by the copy).
- delivery 5 writes one continuation record at
  /projects/mischief/docs-audit/2026-11-25-175724-delivery5 after this commit lands,
  disclosing the delivery 4 reporting gap there.

## Conclusion

Nothing to file; nothing to amend; nothing to re-triage. The board already owns every
premise this tick can produce; all four re-verified at the published tip.

Redaction note: this report is deliberately token-free of trace-marker forms; marker
presence is asserted via the extractor census above so no marker census over the evidence
tree counts this file as an authored marker.

STATUS: PARTIAL
TIER: T2 — premise re-probes are static source/docs greps; two legs ran live (scheduler DB
read, board validate) but the findings' own runtime tiers are inherited from delivery 2's
committed artifact, not re-earned here
EVIDENCE: .coding-hermes/audits/mischief-docs-2026-11-25-17-57-24/audit-recovery-delivery5.md
(this file) + audit-evidence.md sha256 d063b9892992434c80b149afaa32f9f3e79b07e478bf2210a79e7851f58b6288
