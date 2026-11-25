# mischief docs audit transcript - tick mischief-perf-2026-11-25-17-57-24, delivery 8
# captured: 2026-11-25T14:26:32-05:00 host=karaHermes-mde-7840hs

## 1. identity and publication state
$ git rev-parse HEAD
959f2339a6d1e1b8f58b98876d459cbe97facf54
$ git rev-list --left-right --count origin/main...HEAD
0	0
$ git status --porcelain
?? .coding-hermes/audits/mischief-docs-2026-11-25-17-57-24/audit-transcript-delivery8.md
[end - empty means clean]
$ git ls-remote origin refs/heads/main
959f2339a6d1e1b8f58b98876d459cbe97facf54	refs/heads/main

## 2. four open owner rows at origin/main
$ git show origin/main:.coding-hermes/board/tasks.jsonl | jq -r 'select(.id|test("^DOC-17-R2-R2$|^DOC-34-R2$|^DOC-11-R2$|^DOC-16-R2$")) | [.id,.status,.priority,(.title|.[0:40])] | @tsv'
DOC-17-R2-R2	pending	P4	P4 — operator.md env inventory misses MI
DOC-34-R2	pending	P4	P4 — the reverter hold journal (reverter
DOC-11-R2	pending	P4	P4 — docs/LANES.md claims 21 enabled sch
DOC-16-R2	pending	P4	P4 — PRD §6.1 verb table omits four ship

## 3. premise re-probe: env inventory (owner DOC-17-R2-R2)
$ grep -rhoE '"(MISCHIEF[A-Z0-9_]*|MCFAULT[A-Z0-9_]*)"' --include='*.go' cmd internal | sort -u | wc -l
13
$ grep -c MISCHIEFD_TEST_OWNER_CHILD_DIR docs/reference/operator.md
0
$ grep -c MISCHIEF_REVERTER_SYSTEMD docs/reference/operator.md   # control: a documented sibling
1
$ sed -n 50p README.md
| [docs/reference/operator.md](docs/reference/operator.md) | operator reference: every `MISCHIEF*`/`MCFAULT*` environment variable with its default, safe override values, supported-knob vs internal-only status, and troubleshooting notes |

## 4. premise re-probe: reverter journal (owner DOC-34-R2)
$ grep -cE 'RecordType = "' internal/reverter/record.go
6
$ grep -rn reverter.jsonl README.md CONTRIBUTING.md SECURITY.md docs/*.md docs/reference/*.md docs/prd/*.md | wc -l
0
$ grep -c reverter.jsonl skills/mischief-usage/SKILL.md
1

## 5. premise re-probe: lane count (owner DOC-11-R2)
$ sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT count(*) FROM projects WHERE name LIKE 'mischief%';"
11
$ sed -n 4p docs/LANES.md
21 enabled rows shown below. The DB (`projects` table in
$ sed -n 52p README.md
| [docs/LANES.md](docs/LANES.md) | lane doctrine: mischief's 21 scheduler rows — admission modes, cooldowns, workdirs — and why the scheduler DB (not `fleet.toml`) is the source of truth |

## 6. premise re-probe: PRD verb table (owner DOC-16-R2)
$ grep -c 'mischief arm\|mischief land\|profile run\|profile list' docs/prd/mischief-v0.1.md
0
$ sed -n 155,158p docs/prd/mischief-v0.1.md
### 6.1 Verbs (parameters, not verb names)

Status reconciled against v0.1.0 (`cmd/mischief/main.go` dispatch +
`make bin && ./bin/mischief --help`): **shipped** rows are in the current binary;

## 7. NEW finding: version verb flags vs the shipped CLI table
$ grep -n "^version" skills/mischief-usage/SKILL.md
50:version                      [--json]                              # stamped git sha (tag on release builds); --json emits {"version":..,"tool":"mischief"}
$ ./bin/mischief version --json; echo rc=$?
{"version":"959f2339a6d1","tool":"mischief"}
rc=0
$ ./bin/mischief version --version; echo rc=$?
mischief 959f2339a6d1
rc=0
$ ./bin/mischief version --help; echo rc=$?
mischief 959f2339a6d1
rc=0
$ ./bin/mischief version --nope; echo rc=$?
version: unknown flag --nope; supported: --json, --version
rc=2
$ python3 tools/check_cli_table.py | grep -E "^(PASS|FAIL|WARN|NOTE) version|^cli-table"; echo rc=$?
WARN version — binary accepts undocumented flag(s): version (flag-gap warning, exit 0: the verb's --help does not list them; discovered by probing with an unknown flag — the table must catch up)
cli-table: PASS — 24 command(s), 0 mismatch(es), 1 warning(s)
$ git log --oneline -1 -S "--json" -- skills/mischief-usage/SKILL.md
f599504 docs(mischief): DOC-24-R3 fold-tick bullet states DF-59 row status truthfully; DOC-20-R2 version verb lists --json

## 8. repository doc gate
$ make docs-check (tail)
PASS plane fold (1 flags)
PASS selftest (4 flags)
WARN version — binary accepts undocumented flag(s): version (flag-gap warning, exit 0: the verb's --help does not list them; discovered by probing with an unknown flag — the table must catch up)
cli-table: PASS — 24 command(s), 0 mismatch(es), 1 warning(s)

## 9. board validation
$ boardctl -C /home/kara/mischief validate (tail)
[warn] events.jsonl line 471: event id "EVT-PM-AC-REPAIR-20261104" is not a JSON number (legacy shape tolerated)
RESULT: OK (267 warning(s))

## 10. audit event idempotency
$ git show origin/main:.coding-hermes/board/events.jsonl | grep -c mischief-perf-2026-11-25-17-57-24
1

## 11. published audit evidence digest
$ git show origin/main:.coding-hermes/audits/mischief-docs-2026-11-25-17-57-24/audit-evidence.md | sha256sum
d063b9892992434c80b149afaa32f9f3e79b07e478bf2210a79e7851f58b6288  -
# end of transcript
