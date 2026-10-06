# LANES — mischief's scheduler registration (MSF-022)

The mischief project is registered in the coding-hermes fleet scheduler as one
primary foreman lane plus nine satellite lanes. The DB (`projects` table in
`~/.hermes/coding-hermes/scheduler.db`) is the source of truth; the
`[[projects]]` stanzas in `~/.hermes/fleet.toml` are a pure mirror written by
`fleet-sync.py --write` (DB → file, one direction, no correction rules), so a
lane that is enabled in the DB survives every policy regen by construction.

## The lane set (verified live 2026-10-06, GET /api/v1/projects)

| lane | namespace | admission | cooldown (s) | workdir |
|---|---|---|---|---|
| mischief-foreman | coding-hermes | tasks | 21600 | /home/kara/mischief |
| mischief-qa | qa | cooldown | 43200 | ~/.hermes/stand-in/pm/mischief |
| mischief-pm | pm | cooldown | 86400 | ~/.hermes/stand-in/pm-lane/mischief |
| mischief-dogfood | dogfood | cooldown | 259200 | ~/.hermes/stand-in/dogfood/mischief |
| mischief-sync | duckbrain-sync | cooldown | 21600 | ~/.hermes/sync-workdirs/mischief-sync |
| mischief-releng | releases | cooldown | 86400 | ~/.hermes/stand-in/releng-lane/mischief |
| mischief-review | review | cooldown | 604800 | ~/.hermes/stand-in/review/mischief |
| mischief-docs | docs | cooldown | 604800 | ~/.hermes/stand-in/docs/mischief |
| mischief-readme | docs | cooldown | 604800 | ~/.hermes/stand-in/readme/mischief |
| mischief-chaos | monitoring | cooldown | 604800 | ~/.hermes/stand-in/monitoring/mischief |

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
- MSF-032 (this board): the sanction gate covers `plan` only — `selftest` and
  `battery` run unsanctioned (measured). Until that lands, the chaos lane's
  prompt is the enforcement layer.

ch:trace row=MSF-022 evidence=docs/chaos/MSF-022/ witness=live:GET /api/v1/projects/mischief-chaos enabled=true + fleet.toml stanza post fleet-sync --write
