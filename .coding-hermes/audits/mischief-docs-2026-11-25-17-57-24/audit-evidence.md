# mischief docs gap audit — frozen probe transcript

- tick: mischief-perf-2026-11-25-17-57-24 (mischief-docs lane)
- audited tip: c551203881f4691061c3c60aa9dd104c60e69b9d (main == origin/main, clean checkout, 0/0 ahead-behind after fetch)
- probe root: /home/kara/mischief (canonical checkout); binary built fresh by `make docs-check` at the same tip: `bin/mischief (mischief c551203881f4, toolchain go1.26.9)`
- all commands run from /home/kara/mischief unless noted

## Repo gates first (what the gates already guard)

```
$ make docs-check
toolchain: go1.26.9 from the module cache ... (satisfies go.mod's toolchain go1.26.9)
bin: bin/mischief (mischief c551203881f4, toolchain go1.26.9)
python3 tools/check_row_drift.py
row-drift: scanned 2 prose file(s), resolved 29 row reference(s) against 261 board rows
row-drift: PASS — no closed row cited as current behaviour
python3 tools/gen-docs.py --check
OK — 0 stale twin(s)
python3 tools/check_cli_table.py
WARN version — binary accepts undocumented flag(s): version (flag-gap warning, exit 0: the verb's --help does not list them; discovered by probing with an unknown flag — the table must catch up)
cli-table: PASS — 24 command(s), 0 mismatch(es), 1 warning(s)
```

Gate coverage: CLI verb/flag table vs skills/mischief-usage/SKILL.md (24 commands, PASS; the
`version` warning is structural to the accepts-anything behavior measured by completed
MISCHIEF-DF-72), board-row-reference drift (row-drift PASS), generated HTML twins (OK).
NOT guarded: env-var inventory prose, the reverter hold journal schema, scheduler-lane
tables, the PRD verb table.

## FINDING A — operator.md env inventory claim misses MISCHIEFD_TEST_OWNER_CHILD_DIR (recur of DOC-17 / DOC-17-R2)

The doc's own construction claim (docs/reference/operator.md:8-12): the inventory grep
"must resolve to exactly the variable sections below", with bare `MISCHIEF` named as its
one non-variable hit.

```
$ grep -rhoE '"(MISCHIEF[A-Z0-9_]*|MCFAULT[A-Z0-9_]*)"' --include='*.go' cmd internal | sort -u
"MCFAULT"
"MCFAULT_BUDGET"
"MCFAULT_PROOF"
"MISCHIEF"
"MISCHIEFD_METRICS_ADDR"
"MISCHIEFD_TEST_OWNER_CHILD_DIR"
"MISCHIEF_REVERTER_DETACH_MODE"
"MISCHIEF_REVERTER_DIR"
"MISCHIEF_REVERTER_EXPIRY"
"MISCHIEF_REVERTER_HOLD"
"MISCHIEF_REVERTER_SYSTEMD_RUN_FAKE"
"MISCHIEF_SANCTION"
"MISCHIEF_SANCTION_FILE"
```

13 hits; 11 have sections in operator.md; TWO are not sections: bare `MISCHIEF`
(explained at operator.md:11-12 as the internal/battery/findingid_test.go fixture) and
`MISCHIEFD_TEST_OWNER_CHILD_DIR`, which has NO section and NO mention anywhere in the file:

```
$ grep -rn 'MISCHIEFD_TEST_OWNER_CHILD_DIR' --include='*.go' cmd internal
internal/daemon/owner_test.go:22:const ownerTestChildEnv = "MISCHIEFD_TEST_OWNER_CHILD_DIR"
$ grep -c 'MISCHIEFD_TEST_OWNER_CHILD_DIR' docs/reference/operator.md
0
```

Drift attribution (post-close: the DOC-17-R2 fix predates the constant):

```
$ git log -1 --format='%h %ci %s' -S'MISCHIEFD_TEST_OWNER_CHILD_DIR' -- internal/daemon/owner_test.go
dd93c46 2026-11-20 07:26:28 -0500 MISCHIEF-DF-71: refuse a second serve on an owned run dir and pin single-revert-per-hold folding. Addresses MISCHIEF-DF-71.
$ git log -1 --format='%h %ci %s' -- docs/reference/operator.md
53e27f3 2026-11-18 14:51:19 -0500 doc-17-r2: add MISCHIEFD_METRICS_ADDR section, scope env-coverage claim. Addresses DOC-17-R2.
```

## FINDING B — the reverter hold journal (reverter.jsonl) schema is documented nowhere (residual of DOC-34)

internal/reverter/record.go declares a SECOND journal vocabulary distinct from the
experiment journal OPERATIONS.md documents:

```
$ grep -nE 'RecordType = ' internal/reverter/record.go | head
	RecordArm RecordType = "arm"
	RecordLand RecordType = "land"
	RecordLandRefused RecordType = "land_refused"
	RecordHoldExpired RecordType = "hold_expired"
	RecordRevertProof RecordType = "revert_proof"
	RecordHoldStuck RecordType = "hold_stuck"
```
(6 types; envelope `record` at internal/reverter/record.go:442-470: type, hold_id, ts,
boot_id, own_pid?, outcome?, payload (arm only), fields?; validated at the write path.)

```
$ grep -n 'journalFileName' internal/reverter/journal.go | head -2
19:const journalFileName = "reverter.jsonl"
218:	// Dir is the state directory (the journal lives at Dir/reverter.jsonl).
```

Reader-facing corpus coverage of those six type strings and the file name:

```
$ grep -rn 'revert_proof\|land_refused\|hold_expired\|hold_stuck' README.md CONTRIBUTING.md SECURITY.md docs/*.md docs/reference/*.md skills/mischief-usage/SKILL.md
skills/mischief-usage/SKILL.md:105:writes a `revert_proof` line (`outcome` + `reason` + `detail=check: same` /
$ grep -rn 'reverter.jsonl' README.md CONTRIBUTING.md SECURITY.md docs/*.md docs/reference/*.md docs/prd/*.md skills/
skills/mischief-usage/SKILL.md:378:`<run>/reverter.jsonl`. As of HEAD c28800d the DF-39 crash is gone but the
```

(OPERATIONS.md grepped separately for the same tokens plus prd/test-plane: only
prd:166 mentions `journal.jsonl` as the planned `run` verb's out path — not this journal.)
CONTROL (the same grep family finds what IS documented): the experiment-journal types are
documented in docs/OPERATIONS.md:290-306 — so the zero above is a real gap, not a blind
checker. docs/OPERATIONS.md:259 sends backup/restore operators at "the run-dir journal
(.jsonl records)" without naming or documenting this journal; `status` folds it.

## FINDING C — LANES.md claims 21 enabled scheduler rows; the live DB returns 11 (recur of DOC-11)

docs/LANES.md:4 "The mischief project is registered ... as the 21 enabled rows shown
below", table at :17-39, self-described complete output of the documented query (:44).

The doc's own regeneration query, run live against the doc's own cited DB path:

```
$ sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT count(*) FROM projects WHERE name LIKE 'mischief%';"
11
$ sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT name, enabled FROM projects WHERE name LIKE 'mischief%' ORDER BY name;"
mischief-docs|1
mischief-dogfood|1
mischief-foreman|1
mischief-ops|1
mischief-perf|1
mischief-pm|1
mischief-qa|1
mischief-readme|1
mischief-releng|1
mischief-review|1
mischief-sync|1
```

Set arithmetic over table (21 rows, LANES.md:19-39) vs live (11): intersection 9
(docs, dogfood, foreman, pm, qa, readme, releng, review, sync); documented-but-gone 12
(mischief-chaos, -chaos-dogfood, -chaos-pm, -chaos-qa, -chaos-releng, -chaos-releng-qa,
-chaos-sync; mischief-docs-qa, -foreman-qa, -readme-qa, -review-qa, -releng-qa);
live-but-absent 2 (mischief-ops, mischief-perf). 9+12=21 and 9+2=11.

README.md:52 carries the same stale count in its docs index: "lane doctrine: mischief's
21 scheduler rows — admission modes, cooldowns, workdirs".

Last refresh attribution:

```
$ git log -1 --format='%h %ci %s' -- docs/LANES.md
e21d22d 2026-10-24 10:14:23 -0500 docs: refresh mischief lane inventory. Addresses DOC-11.
```

The chaos-family retirement and the ops/perf lane additions both postdate that refresh.
docs/CHAOS-LANE.md is doctrine (per-project pattern) and makes no live-registration claim;
not a finding.

## FINDING D — the PRD verb table (§6.1) omits four shipped verbs: arm, land, profile run, profile list (recur of DOC-16)

The binary ships 24 commands (cli-table gate PASS list at this tip: plan, status, revert,
arm, land, serve, doctor, battery, chaos matrix/check/finding, sim, profile run, profile
list, install, uninstall, audit, retention, plane parse-agent-id/record/selftest-verdict/fold,
selftest, version). docs/prd/mischief-v0.1.md §6.1 (line 157: "Status reconciled against
v0.1.0 (cmd/mischief/main.go dispatch + make bin && ./bin/mischief --help)") lists 15
shipped verb rows. README.md:147 directs readers to "the verb table in the PRD".

```
$ grep -n 'mischief arm\|mischief land\|profile run\|profile list' docs/prd/mischief-v0.1.md
(no output — zero mentions anywhere in the PRD)
```

Attribution:
```
$ git log -1 --format='%h %ci %s' -S'"arm"' -- cmd/mischief/main.go
8137f85 2026-10-14 07:47:14 -0500 df-05: add mischief arm verb to write reverter holds. Addresses DF-05.
$ git log -1 --format='%h %ci %s' -S'"land"' -- cmd/mischief/main.go
4015197 2026-11-03 17:37:48 -0500 PROF-002: a fault that never landed can no longer grade `reverted`
$ git log -1 --format='%h %ci %s' -S'"profile"' -- cmd/mischief/main.go
bf55df1 2026-11-20 07:28:40 -0500 PROF-003: implement profile run executor with finding JSON and graded exit codes. Addresses PROF-003.
$ git log -1 --format='%h %ci %s' -- docs/prd/mischief-v0.1.md
b5c7be1 2026-10-24 08:44:48 -0500 docs: reconcile PRD CLI verb surface. Addresses DOC-16.
```

arm predates the DOC-16 reconcile (the reconcile missed it); land and profile landed after
it. tools/check_cli_table.py guards skills/mischief-usage/SKILL.md only — no gate covers
the PRD table.

## Verified-clean surfaces this pass (no findings)

- README fresh-host behavior, live: `HOME=<fresh> bin/mischief status` prints exactly
  `run_dir: /tmp/mc-docsaudit-20261125/fresh-home/.mischief/runs/default` + `state: absent`,
  exit 1, wrote nothing (README:110-119 claim holds).
- `bin/mischief version --json` → `{"version":"c551203881f4","tool":"mischief"}`, exit 0
  (README:222 holds; stamped at the audited tip).
- SECURITY.md:20 "an unstamped plain `go build` reports `mischief unknown`" — rebuilt
  unstamped to /tmp and ran: prints `mischief unknown`, exit 0. Claim holds (DOC-23 fix).
- `bin/mischief audit` live: `condition: not_installed`, exit 0; `--json` carries the same
  condition — README:270-285 exit ladder holds.
- health.json struct (internal/daemon/daemon.go:46-95) matches docs/OPERATIONS.md's 11-key
  table and holds[] element keys exactly, including omitempty semantics.
- Metrics families (internal/daemon/metrics.go:182-223) match docs/OPERATIONS.md:199-207
  (7 families, labels as documented).
- Cited line anchors: internal/journal/record.go:52-67 (Record struct) and :16-34 (type
  vocabulary), internal/verdict/engine.go:379 (Result.Record), internal/journal/experiment.go:419
  (RunID) — all resolve to the documented content at this tip.
- docs/FAULT-CATALOG.md row census: 113 layer-ID table rows = 104 primitives + 9 simulated
  (23 I-rows = 14 + 9); README:43's counts hold; catalog/faults carries 18 descriptor files
  and FAULT-CATALOG.md:251 says eighteen (DOC-32 fix holds).
- CONTRIBUTING.md CI claims vs .github/workflows/ci.yml steps (Build, Vet, Docs twins,
  CLI table gate, Test, Selftest suite, Battery live cell, Shim .so live landing proof,
  Shim drift gate, Row-status drift gate) — consistent (DOC-14 fix holds).
- docs/EXPERIMENTS.md: catalog/experiments carries exactly 4 experiment yamls as documented.
- TEST-PLANE env vars (BUNKER_PLANE_*) present in tree scripts and documented (10 mentions
  in docs/TEST-PLANE.md; DOC-13 fix holds).
- Board hygiene: no open doc rows; only pending rows are RELEASE-013 (P1 release sweep,
  functional) and NEVER-DONE (perpetual fixture). No doc rows filed at P1-P3 before the
  2026-09-30 ruling remain open, so no re-triage work this tick.

Redaction note: this transcript is deliberately token-free of trace-marker forms; census
sentence above avoids the marker token on purpose.
