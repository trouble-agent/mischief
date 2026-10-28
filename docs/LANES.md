# LANES — mischief's scheduler registration (MSF-022)

The mischief project is registered in the coding-hermes fleet scheduler as the
21 enabled rows shown below. The DB (`projects` table in
`~/.hermes/coding-hermes/scheduler.db`) is the source of truth; the
`[[projects]]` stanzas in `~/.hermes/fleet.toml` are a pure mirror written by
`fleet-sync.py --write` (DB → file, one direction, no correction rules), so a
lane that is enabled in the DB survives every policy regen by construction.

## The lane set (verified live 2026-10-24, scheduler DB query)

The table is the complete output of the documented query below. The blank
`admission_mode` cells are the fleet-generated `-qa` child rows; they are still
included explicitly so the DB result has no unexplained rows. `cooldown_pin_s`
is shown separately from `cooldown_s` because the scheduler stores both values.

| lane | admission_mode | cooldown_s | enabled | workdir | cooldown_pin_s |
|---|---|---:|---:|---|---:|
| mischief-chaos | tasks | 21600 | 1 | /home/kara/.hermes/stand-in/monitoring/mischief | 21600 |
| mischief-chaos-dogfood | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/dogfood/mischief-chaos | 21600 |
| mischief-chaos-pm | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/pm-lane/mischief-chaos | 21600 |
| mischief-chaos-qa | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/pm/mischief-chaos | 21600 |
| mischief-chaos-releng | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/releng-lane/mischief-chaos | 21600 |
| mischief-chaos-releng-qa |  | 43200 | 1 | /home/kara/.hermes/stand-in/pm/mischief-chaos-releng | 43200 |
| mischief-chaos-sync | cooldown | 21600 | 1 | /home/kara/.hermes/sync-workdirs/mischief-chaos-sync | 21600 |
| mischief-docs | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/docs/mischief | 604800 |
| mischief-docs-qa |  | 43200 | 1 | /home/kara/.hermes/stand-in/pm/mischief-docs | 43200 |
| mischief-dogfood | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/dogfood/mischief | 259200 |
| mischief-foreman | tasks | 21600 | 1 | /home/kara/mischief | 21600 |
| mischief-foreman-qa |  | 43200 | 1 | /home/kara/.hermes/stand-in/pm/mischief-foreman | 43200 |
| mischief-pm | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/pm-lane/mischief | 86400 |
| mischief-qa | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/pm/mischief | 43200 |
| mischief-readme | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/readme/mischief | 604800 |
| mischief-readme-qa |  | 43200 | 1 | /home/kara/.hermes/stand-in/pm/mischief-readme | 43200 |
| mischief-releng | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/releng-lane/mischief | 86400 |
| mischief-releng-qa |  | 43200 | 1 | /home/kara/.hermes/stand-in/pm/mischief-releng | 43200 |
| mischief-review | cooldown | 21600 | 1 | /home/kara/.hermes/stand-in/review/mischief | 604800 |
| mischief-review-qa |  | 43200 | 1 | /home/kara/.hermes/stand-in/pm/mischief-review | 43200 |
| mischief-sync | cooldown | 21600 | 1 | /home/kara/.hermes/sync-workdirs/mischief-sync | 21600 |

The exact regeneration query is:

```sh
sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT name, admission_mode, cooldown_s, enabled, workdir, COALESCE(cooldown_pin_s,'-') FROM projects WHERE name LIKE 'mischief%' ORDER BY name;"
```

Its live output at this commit is:

```text
mischief-chaos|tasks|21600|1|/home/kara/.hermes/stand-in/monitoring/mischief|21600
mischief-chaos-dogfood|cooldown|21600|1|/home/kara/.hermes/stand-in/dogfood/mischief-chaos|21600
mischief-chaos-pm|cooldown|21600|1|/home/kara/.hermes/stand-in/pm-lane/mischief-chaos|21600
mischief-chaos-qa|cooldown|21600|1|/home/kara/.hermes/stand-in/pm/mischief-chaos|21600
mischief-chaos-releng|cooldown|21600|1|/home/kara/.hermes/stand-in/releng-lane/mischief-chaos|21600
mischief-chaos-releng-qa||43200|1|/home/kara/.hermes/stand-in/pm/mischief-chaos-releng|43200
mischief-chaos-sync|cooldown|21600|1|/home/kara/.hermes/sync-workdirs/mischief-chaos-sync|21600
mischief-docs|cooldown|21600|1|/home/kara/.hermes/stand-in/docs/mischief|604800
mischief-docs-qa||43200|1|/home/kara/.hermes/stand-in/pm/mischief-docs|43200
mischief-dogfood|cooldown|21600|1|/home/kara/.hermes/stand-in/dogfood/mischief|259200
mischief-foreman|tasks|21600|1|/home/kara/mischief|21600
mischief-foreman-qa||43200|1|/home/kara/.hermes/stand-in/pm/mischief-foreman|43200
mischief-pm|cooldown|21600|1|/home/kara/.hermes/stand-in/pm-lane/mischief|86400
mischief-qa|cooldown|21600|1|/home/kara/.hermes/stand-in/pm/mischief|43200
mischief-readme|cooldown|21600|1|/home/kara/.hermes/stand-in/readme/mischief|604800
mischief-readme-qa||43200|1|/home/kara/.hermes/stand-in/pm/mischief-readme|43200
mischief-releng|cooldown|21600|1|/home/kara/.hermes/stand-in/releng-lane/mischief|86400
mischief-releng-qa||43200|1|/home/kara/.hermes/stand-in/pm/mischief-releng|43200
mischief-review|cooldown|21600|1|/home/kara/.hermes/stand-in/review/mischief|604800
mischief-review-qa||43200|1|/home/kara/.hermes/stand-in/pm/mischief-review|43200
mischief-sync|cooldown|21600|1|/home/kara/.hermes/sync-workdirs/mischief-sync|21600
```

In particular, `mischief-chaos` is admission_mode `tasks` with live
`cooldown_s=21600` and `cooldown_pin_s=21600`; it is not a cooldown lane at
604800 seconds.

Every satellite's `.coding-hermes/board` is a symlink to the primary's board
(`/home/kara/mischief/.coding-hermes/board`) — satellites read the primary's
backlog and file rows into it; only the foreman dispatches work. All lanes
deliver to the mischief Telegram thread
(`telegram:-1003310984808:168443`); the foreman was created 2026-10-02 with
the family, and the row's `namespace_id` points at scheduler ROLE namespaces
(the mischief knowledge-store namespace lives in DuckBrain, not in the
scheduler DB — that split is the fleet convention, not a defect).

## The chaos satellite (the distinctive one)

`mischief-chaos` is the fault-matrix cadence lane. Its isolation contract is
MSF-020/SPEC-13, enforced in the lane prompt, not by hope: it never runs a
fault on the scheduler's own host; every primitive lands on the ephemeral
bunker agent that `scripts/bunker-plane.sh` spawns and destroys. The lane's
whole tick is:

```sh
BUNKER_PLANE_REPO=/home/kara/mischief bash /home/kara/mischief/scripts/bunker-plane.sh run
```

The tiny matrix is `selftest --all` on the agent: 21 cells, 13 land+revert
proofs, 8 named capability skips (the L2/L3 classes TEST-PLANE §5 lists as
structurally out of reach on a rootless agent), 0 failures on the measured
baseline. First live run: `docs/chaos/MSF-022/` (evidence JSONL, selftest
journal with landed-proof counters, admission report — run_id 63761a4aa1478ce1,
agent destroyed with zero residue). A bunker that is offline or full is a
named SKIP for the tick, never a retry loop.

## Registration traps this row measured (filed, not fixed here)

- SCHED-GAP-1729 (scheduler board): the projects API classifies a new lane
  foreman-class while `namespace_id` is still NULL, so the admission law
  refuses `admission_mode=cooldown` on the namespace PUT; and POST births the
  row ARMED at the 900 s default. Provisioning order that works today:
  create → cooldown triple → namespace+deliver → admission_mode → enable.
- MSF-032 (this board, landed): the sanction gate covers ALL fault-landing
  verbs — `plan`, `arm`, `selftest`, `battery`, `sim`, `chaos` — each verb
  file carries a `sanction.Check` call, so the chaos lane's `selftest` and
  `battery` legs run SANCTIONED, not unsanctioned.

ch:trace row=DOC-11 spec=docs/LANES.md#the-lane-set wave=mischief-foreman-2026-10-24-14-52-45#DOC-11 test=sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT name, admission_mode, cooldown_s, enabled, workdir, COALESCE(cooldown_pin_s,'-') FROM projects WHERE name LIKE 'mischief%' ORDER BY name;" evidence=docs/LANES.md witness=none:live-db-query-and-doc-table-reconciled verdict=none:pending commit=none:pending memory=none:not-written

ch:trace row=DOC-12 spec=none:docs-row wave=mischief-foreman-2026-10-28-19-31-37.json#DOC-12 test=none:docs-only-greps doc=docs/LANES.md prompt=/tmp/mf-20261028-1931-DOC-12-brief.txt evidence=commit witness=none:docs-greps verdict=none:pending-foreman-judge commit=self memory=none:board-row
STATUS: complete
TIER: T1 (edits + greps invoked and observed)
EVIDENCE: commit
