# LANES — mischief's scheduler registration (MSF-022)

The mischief project is registered in the coding-hermes fleet scheduler as the
11 enabled rows shown below (live-verified 2026-11-26 by direct query of
`projects WHERE name LIKE 'mischief%'` in `~/.hermes/coding-hermes/scheduler.db`).
The DB (`projects` table in
`~/.hermes/coding-hermes/scheduler.db`) is the source of truth; the
`[[projects]]` stanzas in `~/.hermes/fleet.toml` are a pure mirror written by
`fleet-sync.py --write` (DB → file, one direction, no correction rules), so a
lane that is enabled in the DB survives every policy regen by construction.

## The lane set (verified live 2026-11-26, scheduler DB query)

The table is the complete output of the documented query below, filtered to the
11 `enabled = 1` rows. The earlier `-qa` child rows and the `mischief-chaos*`
satellite rows that the 2026-10-24 snapshot listed are no longer in the DB
registration (retired); the chaos satellite's doctrine and its measured
baseline remain documented in `docs/CHAOS-LANE.md`. `cooldown_pin_s` is shown
separately from `cooldown_s` because the scheduler stores both values.

| lane | admission_mode | cooldown_s | enabled | workdir | cooldown_pin_s |
|---|---|---:|---:|---|---:|
| mischief-docs | cooldown | 604800 | 1 | /home/kara/.hermes/stand-in/docs/mischief | 604800 |
| mischief-dogfood | cooldown | 259200 | 1 | /home/kara/.hermes/stand-in/dogfood/mischief | 259200 |
| mischief-foreman | tasks | 21600 | 1 | /home/kara/mischief | 21600 |
| mischief-ops | tasks | 86400 | 1 | /home/kara/.hermes/stand-in/pm/mischief | 86400 |
| mischief-perf | cooldown | 604800 | 1 | /home/kara/.hermes/stand-in/perf/mischief | 604800 |
| mischief-pm | cooldown | 86400 | 1 | /home/kara/.hermes/stand-in/pm-lane/mischief | 86400 |
| mischief-qa | cooldown | 43200 | 1 | /home/kara/.hermes/stand-in/pm/mischief | 43200 |
| mischief-readme | cooldown | 604800 | 1 | /home/kara/.hermes/stand-in/readme/mischief | 604800 |
| mischief-releng | cooldown | 86400 | 1 | /home/kara/.hermes/stand-in/releng-lane/mischief | 86400 |
| mischief-review | cooldown | 604800 | 1 | /home/kara/.hermes/stand-in/review/mischief | 604800 |
| mischief-sync | cooldown | 21600 | 1 | /home/kara/.hermes/sync-workdirs/mischief-sync | 21600 |

The exact regeneration query is:

```sh
sqlite3 -readonly ~/.hermes/coding-hermes/scheduler.db "SELECT name, admission_mode, cooldown_s, enabled, workdir, COALESCE(cooldown_pin_s,'-') FROM projects WHERE name LIKE 'mischief%' ORDER BY name;"
```

Its live output at this writing (2026-11-26) is:

```text
mischief-docs|cooldown|604800|1|/home/kara/.hermes/stand-in/docs/mischief|604800
mischief-dogfood|cooldown|259200|1|/home/kara/.hermes/stand-in/dogfood/mischief|259200
mischief-foreman|tasks|21600|1|/home/kara/mischief|21600
mischief-ops|tasks|86400|1|/home/kara/.hermes/stand-in/pm/mischief|86400
mischief-perf|cooldown|604800|1|/home/kara/.hermes/stand-in/perf/mischief|604800
mischief-pm|cooldown|86400|1|/home/kara/.hermes/stand-in/pm-lane/mischief|86400
mischief-qa|cooldown|43200|1|/home/kara/.hermes/stand-in/pm/mischief|43200
mischief-readme|cooldown|604800|1|/home/kara/.hermes/stand-in/readme/mischief|604800
mischief-releng|cooldown|86400|1|/home/kara/.hermes/stand-in/releng-lane/mischief|86400
mischief-review|cooldown|604800|1|/home/kara/.hermes/stand-in/review/mischief|604800
mischief-sync|cooldown|21600|1|/home/kara/.hermes/sync-workdirs/mischief-sync|21600
```

The family's only `tasks`-mode rows are `mischief-foreman` and `mischief-ops`;
every other enabled row is a cooldown lane. The `mischief-chaos*` satellite
rows of the 2026-10-24 snapshot are retired from this registration.

Every satellite's `.coding-hermes/board` is a symlink to the primary's board
(`/home/kara/mischief/.coding-hermes/board`) — satellites read the primary's
backlog and file rows into it; only the foreman dispatches work. All lanes
deliver to the mischief Telegram thread
(`telegram:-1003310984808:168443`); the foreman was created 2026-10-02 with
the family, and the row's `namespace_id` points at scheduler ROLE namespaces
(the mischief knowledge-store namespace lives in DuckBrain, not in the
scheduler DB — that split is the fleet convention, not a defect).

## The chaos satellite (retired from this registration)

The `mischief-chaos` rows that once made this family's registration distinctive
are no longer in the scheduler DB (2026-11-26 live query: 11 enabled rows, none
of them `mischief-chaos*`). The doctrine is kept below because
`docs/CHAOS-LANE.md` still documents the pattern for any project adopting a
fault-matrix satellite, and the measured baseline is evidence the project owns:

`mischief-chaos` was the fault-matrix cadence lane. Its isolation contract is
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

ch:trace row=DOC-12 spec=none:docs-row wave=mischief-foreman-2026-10-28-19-31-37.json#DOC-12 test=none:docs-only-greps doc=docs/LANES.md prompt=none:brief-not-retained evidence=commit witness=none:docs-greps verdict=none:pending-foreman-judge commit=a45bb1f memory=none:board-row
