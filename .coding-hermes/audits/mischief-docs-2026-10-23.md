# mischief — documentation audit, 2026-10-23

Lane: mischief-docs (tick `mischief-docs-2026-10-23-20-18-53`), stand-in workdir
`~/.hermes/stand-in/docs/mischief`. Target HEAD: `0da29a0` (origin/main, 0 ahead / 0 behind
at tick start and after `git fetch origin`).

Method: `coding-hermes-docs` sweep — surface derived from source and the running binary,
diffed against `docs/` (README left to the README lane, which filed README-10..16 earlier
the same day), then rot and audience checks. Nine findings filed as DOC-11..DOC-19, all P4
by the 2026-09-30 doc-severity ruling.

## Surface exercised (T3/T4 — real commands, this host)

| surface | command run | result |
|---|---|---|
| verb + flag census | `./bin/mischief <verb> --help` for plan, status, arm, revert, doctor, selftest, serve, install, uninstall, audit, retention, battery, sim, version, chaos {matrix,check,finding}, plane {parse-agent-id,record,selftest-verdict,fold} | all rc=0, full flag lists captured (`/tmp`, not committed) |
| experiment plan claims | `MISCHIEF_SANCTION=1 ./bin/mischief plan -f catalog/experiments/ex-00{1..4}.yaml` | exit codes and refusal reasons match `docs/EXPERIMENTS.md` table exactly (0/2/2/2; `scope_below_minimum`, `P-006: not in the loaded catalog`, `protected_target`) — NO finding |
| sanction gate | `./bin/mischief arm ...` unsanctioned, scratch `--dir` | rc=2 `host_not_sanctioned`, names `MISCHIEF_SANCTION_FILE` + `MISCHIEF_SANCTION`, writes nothing (dir absent) |
| serve daemon / health | `serve --dir $(mktemp -d)` + `curl 127.0.0.1:7661/health.json` + `ss -ltn` | health.json written; curl rc=7, no listener → PRD "serves /health.json" is false (DOC-16) |
| battery parity | `MISCHIEF_SANCTION=1 ./bin/mischief battery --quick --dry-run` | 5 cells q1..q5 exactly as `docs/BATTERY-PARITY.md` states — NO finding |
| plane evidence verbs | `plane selftest-verdict --exit-code {0,1,2}`, `plane record`, `plane fold` on pass/fail/skip evidence | prints pass/fail; fold returns 0/1/3 per `docs/TEST-PLANE.md` §6 — NO finding |
| docs twin gate | `python3 tools/gen-docs.py --check` | `FAIL — 5 stale twin(s)`, rc=1 (DOC-15) |
| CI gate claims | `grep -nE 'run:|name:' .github/workflows/ci.yml`; `grep -c gen-docs ci.yml` → 0 | CONTRIBUTING's docs-twin CI gate does not exist (DOC-14) |
| lane set | `sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT ... FROM projects WHERE name LIKE 'mischief%'"` | 21 lanes; `docs/LANES.md` documents 10 from 2026-10-06 and misstates `mischief-chaos` admission/cooldown (DOC-11) |
| test-plane env | `grep -c` per variable in `scripts/bunker-plane.sh` | 2 documented vars read nowhere; 3 read vars undocumented (DOC-13) |
| release flow | `make tags`; `grep` of `release.yml`, `.goreleaser.yml`, Makefile targets | matches `docs/RELEASE.md` — NO finding |
| catalogue counts | `grep -oE '^\| [A-Z]-[0-9]{3}' docs/FAULT-CATALOG.md \| wc -l` → 113; per-layer 12/16/23/18/14/12/14/4 | consistent with README's own note (README-4/15 already filed by the README lane) — not re-filed |
| internal doc references | every backticked `*.go/*.sh/*.yaml/*.md` path in docs tested for existence | no dead source references (hits are runtime artifacts / external `bunker-qa.sh`) |
| spec documents | `find . -iname '*SPEC*'`; grep for `SPEC-0x.md` | only `docs/SPEC-PLAN.md`; CONTRIBUTING names "the shipped spec documents" (DOC-18) |

## Rows filed

| id | finding | artifact |
|---|---|---|
| DOC-11 | LANES lane table stale/incomplete vs scheduler DB; `mischief-chaos` admission mode + cooldown wrong | docs/LANES.md |
| DOC-12 | LANES says the sanction gate covers `plan` only; MSF-032 complete, six verbs call `sanction.Check` | docs/LANES.md |
| DOC-13 | TEST-PLANE env table wrong both ways (2 phantom vars, 3 undocumented) | docs/TEST-PLANE.md |
| DOC-14 | CONTRIBUTING claims a CI docs-twin gate that `ci.yml` never runs | CONTRIBUTING.md |
| DOC-15 | 5 committed HTML twins stale (`gen-docs.py --check` rc=1) | docs/*.html, probe/RESULTS.html |
| DOC-16 | PRD §6.1 flag table drift + "serve serves /health.json" false (measured) | docs/prd/mischief-v0.1.md |
| DOC-17 | operator reference omits `MISCHIEF_SANCTION` / `MISCHIEF_SANCTION_FILE` while claiming completeness | docs/reference/operator.md |
| DOC-18 | CONTRIBUTING points at "shipped spec documents" that exist nowhere in the repo | CONTRIBUTING.md |
| DOC-19 | BATTERY-PARITY lists `battery.jsonl` among `--out` artefacts; journal follows `--dir` | docs/BATTERY-PARITY.md |

## Re-triage of pre-ruling doc rows

Board scan: no open P1-P3 documentation row predates 2026-09-30 (DOC-1..DOC-10 are all
`complete` at P4; the open README-* rows were filed today at P4/P5). Re-triage budget
unspent.

## What this tick did NOT verify

- Did not run `selftest --all`, `battery` (real), `sim`, or any fault-landing path: the
  audit lane does not arm faults. Claims depending on those runs (TEST-PLANE's 21-cell
  baseline, first-live-run counts, chaos-lane matrix) were left as historical records.
- Did not execute `make dist` / goreleaser (toolchain-dependent) — RELEASE.md checked by
  reading the workflow, Makefile and `.goreleaser.yml`, not by cutting an artifact.
- Did not regenerate the stale HTML twins (audit lane must not write docs).
- Did not verify the Go example in `docs/CATALOG.md` by compiling it; checked field and
  function names against `internal/catalog` source instead (all present).
- `skills/mischief-usage/SKILL.md` deferred to the skill-drift rows (MISCHIEF-DF-63 class).

## Environment note (not a doc finding)

`core.hooksPath` is set to `/var/tmp/foreign-hooks` in this checkout, so commits here run
foreign hooks unless bypassed with `git -c core.hooksPath=.git/hooks`. Board commits this
tick used the repo's own hooks.

ch:trace row=DOC-11 spec=docs/LANES.md#the-lane-set wave=none:standalone-audit test=none:documentation-audit doc=.coding-hermes/audits/mischief-docs-2026-10-23.md prompt=~/.hermes/skills/coding-hermes-docs/SKILL.md evidence=.coding-hermes/audits/mischief-docs-2026-10-23.md witness=none:tick-surface-diff verdict=none:nothing-to-judge commit=none:audit-artifact-not-code memory=none:not-written

## Supplementary pass — the surface the first pass deferred (same tick, second session)

The first pass (above) filed DOC-11..DOC-19 and deferred `skills/mischief-usage/SKILL.md`
to the MISCHIEF-DF-63 class. That deferral was wrong: DF-63's guard
(`tools/check_row_drift.py`) resolves row-status references, it does not compare the CLI
table against the binary. The skill is the agent-facing entry doc, so it was audited here.

| surface | command run | result |
|---|---|---|
| skill CLI table vs shipped flags | `for v in plan status arm doctor selftest battery uninstall audit sim; do ./bin/mischief "$v" --help …; done` + the same for chaos/plane subverbs | 26 accepted flags absent from `skills/mischief-usage/SKILL.md` lines 17-40 (DOC-20) |
| the skill's "`--help` does not list it" claim | `./bin/mischief --help \| grep -n selftest`; `grep -n 'mischief selftest' cmd/mischief/{main,help_test}.go` | help lists `selftest` at main.go:153, asserted by help_test.go:99 — the skill's line 109 is false (DOC-21) |
| shipped-loader boundary | `ls catalog/faults/*.yaml \| wc -l` → 11; `doctor` → "loaded (11 primitives …)"; `selftest --primitive I-003` → rc=2 not-in-catalog | the skill sells 104 primitives and never states the 11-id loader floor (DOC-22); README-15 covers README only |
| CHAOS-LANE.md recipe | instantiated `templates/chaos-lane.yaml` into a scratch lane with the doc's own `sed`, symlinked the board, ran `chaos check --lane` | rc=0 "template contract OK" — the documented recipe works, NO finding |
| CHAOS-LANE.md matrix step | `chaos matrix --project mischief --tier-max 1 --out <scratch>` | rc=0, wrote `matrix.json` + `matrix.md` — NO finding |
| battery parity (skill's documented run) | `MISCHIEF_SANCTION=1 ./bin/mischief battery --quick --dry-run --out S --dir S` | 5 cells q1..q5 (N-012, I-002, F-009, R-001, F-009), rc=0 — matches BATTERY-PARITY.md |
| re-triage budget | open rows with id `DOC-*`/`README-*` and priority P1-P3 | none (every open doc row is P4/P5, all post-ruling) — unspent again |

Verified NOT to be a finding: `docs/CATALOG.md`'s Go snippets name only symbols that exist in
`internal/catalog` (the first pass checked field/function names); they are package examples
for an internal package, so an outside module cannot compile them by design, not by rot.

ch:trace row=DOC-22 spec=docs/FAULT-CATALOG.md#layer-i wave=none:standalone-audit test=none:documentation-audit doc=.coding-hermes/audits/mischief-docs-2026-10-23.md prompt=~/.hermes/skills/coding-hermes-docs/SKILL.md evidence=.coding-hermes/audits/mischief-docs-2026-10-23.md witness=none:tick-surface-diff verdict=none:nothing-to-judge commit=none:audit-artifact-not-code memory=none:not-written
