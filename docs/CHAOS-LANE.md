# The -chaos satellite lane — M6 fleet adoption (MSF-017)

Mischief's fault-matrix tooling leaves the lab: each adopting project gets
exactly ONE chaos satellite lane that runs the project's fault matrix on a
cadence and files findings — with **no operator in the loop**.

The scope guard, verbatim from the row: *one lane per project scoped to
THAT project, never a fleet-wide artifact generated N times.* Every
mechanism on this page encodes that guard in code
(`internal/chaos.ValidateLane` refuses any other scope), not just in prose.

The lane convention follows the fleet's own satellite shape: a satellite is
a lane whose workdir carries `.coding-hermes/board` as a **symlink** to the
primary lane's board dir, so the lane's own board scan sees the primary's
pending rows. The chaos satellite adds the isolation contract on top
(MSF-020/MSF-022): **it may not run a fault on the scheduler's own host.**

Companion files: `templates/chaos-lane.yaml` (the template, with the same
rules as comments), `internal/chaos/` (the machine-checked contract), and
`cmd/mischief/chaos.go` (the `mischief chaos` verb).

## 1. The convention, in one pass

```sh
# 1. instantiate the template for ONE project
sed -e "s|<PROJECT>|mischief|g" \
    -e "s|<ABSOLUTE_PATH_TO_PRIMARY_BOARD>|/home/agents/mischief/.coding-hermes/board|g" \
    -e "s|<CHAOS_HOST_NAME>|bunker-qa-1|g" \
    templates/chaos-lane.yaml > <lane-workdir>/chaos-lane.yaml

# 2. wire the board symlink (the lane's board scan sees the PRIMARY's board)
mkdir -p <lane-workdir>/.coding-hermes
ln -s /home/agents/mischief/.coding-hermes/board <lane-workdir>/.coding-hermes/board

# 3. verify: template contract + readlink evidence
mischief chaos check --lane <lane-workdir>/chaos-lane.yaml

# 4. derive the project-scoped matrix (the lane's tick body does this)
mischief chaos matrix --project mischief --tier-max 1 --out <lane-workdir>/matrix

# 5. file a finding as a board row (what a tick does per adverse outcome)
mischief chaos finding --project mischief \
  --board /home/agents/mischief/.coding-hermes/board \
  --title "battery finding: R-001 on mischief (not_recovered) — ..." \
  --reason "quick cell graded not_recovered; journal: ~/.mischief/runs/..." \
  --priority P1 --primitive R-001
```

Registration of the lane with the scheduler (DB row + fleet toml stanza,
namespace, model pins) is MSF-022's surface; this page owns the lane
convention and its machinery.

## 2. The template contract (machine-checked)

`internal/chaos.ValidateLane` enforces — in this order, isolation first:

| # | rule | refusal names |
|---|---|---|
| 1 | the lane host is not a forbidden host | `fleet-main-host` (case-insensitive) + the MSF-020/MSF-022 isolation contract; the template's `isolation.forbidden_hosts` extends the list per lane |
| 2 | the lane host is sanctioned | SPEC-13: the mischief sanction marker is not optional; the runtime re-enforces the real marker (`internal/sanction.Check`) before any landing act |
| 3 | lane name | `<project>-chaos`, exactly — one lane per project, named for it |
| 4 | namespace | each lane runs in its own namespace; empty refuses |
| 5 | no placeholders | an unsubstituted `<PLACEHOLDER>` in lane/project/namespace/primary_board refuses — the template is never run as-is |
| 6 | primary board | absolute path (the symlink needs one) |
| 7 | matrix scope | `project` — a fleet-wide scope refuses (the scope guard, encoded) |
| 8 | tier ceiling | `tier_max` within L0..L5 |
| 9 | findings mode | `rows` — findings land as board rows, no operator |
| 10 | findings board | must equal the primary's board — a lane cannot file findings its foreman never reads |

`mischief chaos check --lane <file>` runs the whole contract, then probes
the board symlink (next section) and prints the evidence. Exit 0 = the
lane is wired; exit 2 = the refusal, verbatim.

## 3. The board symlink — the lane's eyes on the primary's board

The lane workdir's `.coding-hermes/board` MUST be a symlink to the
primary's board dir. `chaos.SymlinkEvidence` probes it and refuses:

- **missing** — "create it as a symlink to the primary's board dir (ln -s)";
- **a real directory** — a SEVERED lane: its board scan would see an empty
  board and silently do nothing. The refusal names the fix;
- **dangling** — the primary's board dir must exist.

The evidence is printed, not assumed:

```text
board symlink: <lane-workdir>/.coding-hermes/board -> /home/agents/mischief/.coding-hermes/board (resolves to /home/agents/mischief/.coding-hermes/board = the primary's board)
```

That line is the readlink verification: the raw `readlink(2)` target AND
the fully-resolved path (`filepath.EvalSymlinks`), checked against the
lane's declared `primary_board` (`chaos.VerifyAgainst` — a symlink to a
foreign board refuses).

## 4. The project-scoped matrix — derived, not regurgitated

`mischief chaos matrix --project <p>` derives the matrix from the
project's OWN capability data:

- input: the catalog's per-primitive `capability` + `tier` fields
  (SPEC-01 descriptors), probed against the chaos host's capability
  registry (the same AC-11 derivation `doctor`/`plan` use — no string
  maps);
- a cell is **included** only when its capability answers ready on the
  chaos host AND its declared minimum tier is at or below the lane's
  `tier_max` ceiling;
- an excluded cell still appears, named `tier_above_ceiling` or
  `capability_absent`, with the missing kind or the ceiling in the reason
  — a NULL carries a reason;
- every included cell targets L0 scratch (the battery's own NOT-LIST
  discipline), so the lane cannot arm a fault against a real target by
  construction;
- the artifact is stamped `mischief.chaos.matrix/v1` with the project and
  scope fields set, written as `matrix.json` + `matrix.md`.

The generator refuses to render anything but a project-scoped matrix
(`chaos.ValidateMatrixConf`): there is no fleet mode to reach.

Execution of the included cells is the battery's job (`mischief battery
--project <p>`, SPEC-10) on the sanctioned chaos host; the lane's tick
runs generation, execution, and the findings filing below.

## 5. Findings as rows — the no-operator path

`mischief chaos finding` files ONE finding as a board-vocabulary row on
the OWNING project's board (through the symlink on a live lane; the board
dir is the primary's). The row shape is the board's own
(`battery.Finding`: id, status `pending`, title, priority P0..P3,
depends_on, plus the chaos fields project/primitive/verdict/reason and
the journal path riding in `reasoning` — the AC-18 evidence chain).

Safety, encoded in `chaos.AppendRows`:

- rows are validated (`battery.ValidateRows`, the boardctl-style
  self-check) BEFORE the board is read or written — a bad row leaves the
  board byte-identical;
- the duplicate-id guard refuses a re-filed finding: the id is
  content-addressed (`MSF-CHAOS-<hex(sha256(project,title,reason))[:12]>`),
  so an identical re-file hits the guard, and a genuinely new outcome
  (new verdict or reason) is a new row. Refuse, never duplicate;
- the append is JSONL-append (`O_APPEND`), one row per line — the same
  wire format every fleet board tool reads.

The row id prefix is `MSF-CHAOS-` regardless of owning project; the
`project` field carries the owner (e.g. a crier finding files
`project: crier` onto crier's own board).

## 6. What a lane tick does (the loop, once wired by MSF-022)

1. `mischief chaos check --lane <lane.yaml>` — refuse to run an unverified lane.
2. `mischief chaos matrix --project <p>` — refresh the project matrix.
3. `mischief battery --project <p> --file-rows` — execute the included cells on the sanctioned chaos host, emit findings.
4. `mischief chaos finding ...` per adverse outcome — file rows onto the owning board.
5. The primary's next board scan sees the rows (through the same board the symlink points at) and dispatches them like any other pending row. No operator.

## 7. Isolation contract — encoded, then documented

The check the row demands lives in the template loader, not the docs:
`ValidateLane` refuses a lane whose host is the scheduler's own host
(`fleet-main-host`, case-insensitive, plus the lane's own
`forbidden_hosts` list), before any other check runs. Defense in depth
behind it: SPEC-13's sanction marker (`internal/sanction.Check`, fail
closed on any verb that could arm or land), the load gate, and the L0
scratch selector discipline — the scheduler's own host is the protected
target, not a lab.

## 8. Verification story

`internal/chaos` and the `chaos` verb tests pin the whole surface: the
template contract refusals (scheduler host, unsanctioned host, fleet
scope, wrong lane name, findings-off-board, placeholders), the symlink
evidence (ok / severed / dangling / missing / foreign board), the matrix
filtering (capability absent and tier-above-ceiling both carry reasons),
and the findings path (validated row filed on a scratch board, duplicate
refused, board byte-count unchanged on refusal).

ch:trace row=MSF-017 spec=.coding-hermes/board/tasks.jsonl (MSF-017) evidence=internal/chaos/ + cmd/mischief/chaos.go + templates/chaos-lane.yaml witness=live:scratch-board-demo
