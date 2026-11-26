# Testing profiles — schema (`mischief.profile/v1`)

A **profile** is the unit of work an agent runs: an ordered set of fault steps
for one application class, each step naming a primitive that already exists in
[`catalog/faults/`](../faults/). `mischief profile run` applies the steps in
order, runs each step's probe, and grades the target into one of three values —
`passed`, `failed`, `inconclusive`. The finding it prints is the product; the
fix-and-re-run loop is the point.

This file is the schema. It is deliberately the same shape as an experiment
([`catalog/experiments/*.yaml`](../experiments/)) — `name`/`id`, a `class`, a
declared requirement set, and a list of fault steps — with the difference that
a profile is addressed by `<class>/<name>` and is meant to be run repeatedly
until it turns green.

```
catalog/profiles/
  README.md            this schema
  <class>/<name>.yaml  one profile per file; the path IS the profile id
```

`mischief profile list` prints every id. The class segment is resolved
explicitly — the tree is read two levels deep (`<class>/<name>.yaml`) and never
walked recursively, mirroring the catalog loader's one-flat-directory rule. Two
files can therefore never claim the same id.

## Fields

| field | required | meaning |
|---|---|---|
| `schema` | yes | must be `mischief.profile/v1`. Any other value is refused by name. |
| `name` | yes | the profile name; must equal the file's base name. |
| `class` | yes | the application class; must equal the directory segment. |
| `version` | yes | the profile's own version (`1.0.0`). Profiles version independently of the binary. |
| `summary` | no | one paragraph of agent-facing intent. |
| `seed` | no | default seed. `--seed N` overrides it. The same profile + seed writes the same scenario bytes and gets the same run id. |
| `requires` | no | requirement entries (see below). An **unmet** entry makes the run `inconclusive`, naming it — never a pass. |
| `steps` | yes | ordered action list; at least one. Order is the contract. |
| `cause_classes` | no | closed vocabulary of failure classes, each with a steering `hint`. |

Unknown keys are refused (`KnownFields`): a misspelled field must not vanish
silently.

### `steps[]`

| field | required | meaning |
|---|---|---|
| `id` | yes | step id, unique within the profile (`short-write-append`). |
| `fault` | yes | the primitive id from `catalog/faults/` (`S-004`). Resolved at **run** time against the loaded catalog. |
| `params` | no | the fault's parameters, flat scalars. Every name must be declared by that primitive's own `params:` block, and a declared enumeration must accept the value. A param the primitive does not declare is `inconclusive`, naming the pair — never silently ignored. |
| `probe` | yes | the probe that grades the step. Closed vocabulary: `payload-complete` (S-004), `commit-durable` (S-010). |
| `cause_class` | no | the failure class a `failed` step routes to. Must be declared in `cause_classes` (referential integrity is checked at load, so an agent never receives a hint that does not exist). |

### `requires[]`

Each entry is `token: prose` (or a bare `token`). The token comes from a closed
vocabulary and is resolved against the host at run time:

| token | met when |
|---|---|
| `cc` | a C compiler is on `PATH` (the shim is generated and built per run). |
| `scratch-target` | the target is an absolute path under the scratch base (`$TMPDIR` or `/tmp`). |
| `durability-target` | the target is a `dbdurability:<fsync|none>:<path>` selector — an absolute database path under the scratch base, and a commit discipline this build ships (`fsync` or `none`). The executor builds and spawns the controlled commit-log database child with that discipline at that path (the `database/durability` profile). |
| `writable-target` | the target's parent directory is writable. |

Any other token — a typo included — is an **unmet** requirement: the run grades
`inconclusive` and names the unknown token and the known set.

### `cause_classes`

```yaml
cause_classes:
  storage:silent-short-write:
    hint: >
      The write path did not loop on a short write...
```

`hint` is required. A `failed` step's `cause_class` + hint are lifted to the
run-level finding, which is how one sentence of steering reaches the agent
without a diagnosis it did not earn.

## Worked example — `storage/short-write`

```yaml
schema: mischief.profile/v1
name: short-write
class: storage
version: 1.0.0
seed: 7
requires:
  - cc: a C compiler on PATH (the fault is an LD_PRELOAD shim, built per run)
  - scratch-target: the target must be an absolute path under the scratch base
  - writable-target: the target's parent directory must be writable
steps:
  - id: short-write-append
    fault: S-004
    params:
      path_substr: mischief-profile-target
      fraction: "0.5"
      budget: "1"
    probe: payload-complete
    cause_class: storage:silent-short-write
cause_classes:
  storage:silent-short-write:
    hint: >
      The write path did not loop on a short write.
```

Run it:

```sh
mischief profile run storage/short-write \
  --target scratch:/tmp/mischief-profile-target \
  --json --seed 7
```

## Worked example — `database/durability`

```yaml
schema: mischief.profile/v1
name: durability
class: database
version: 1.0.0
seed: 11
requires:
  - cc: a C compiler on PATH (the fsync-lie shim and the controlled database are built per run)
  - durability-target: the target must be dbdurability:<fsync|none>:<absolute path under the scratch base>
  - writable-target: the target's parent directory must be writable
steps:
  - id: commit-ack-durability
    fault: S-010
    params:
      commits: "3"
    probe: commit-durable
    cause_class: durability:storage-ack-not-durable
cause_classes:
  durability:storage-ack-not-durable:
    hint: >
      The target acknowledged durability stable storage never confirmed...
```

Run it (the fsync-on-commit target grades `passed`; the no-fsync discipline
grades `failed` with the cause class above — the same profile, two targets):

```sh
mischief profile run database/durability \
  --target dbdurability:fsync:/tmp/mischief-durability.db \
  --json --seed 11    # exit 0 (passed)
mischief profile run database/durability \
  --target dbdurability:none:/tmp/mischief-durability-none.db \
  --json --seed 11    # exit 1 (failed: durability:storage-ack-not-durable)
```

The target is a real commit-log database process the executor builds and
spawns per run (a compiled binary with a real database file and a real ack
ledger). The injected S-010 lie models the volatile write-back layer inside
the child's memory: the commit child's exit is the simulated power loss, and
the parent's post-exit read of the database file is the external loss
witness — the finding's notes name this posture on every run.

## The finding (`mischief.finding/v1`)

One JSON document, nothing else on stdout with `--json`:

```json
{
  "schema": "mischief.finding/v1",
  "profile": "storage/short-write",
  "profile_class": "storage",
  "profile_version": "1.0.0",
  "run_id": "…",
  "run_dir": "…/<run_id>",
  "seed": 7,
  "target": {"kind": "scratch", "selector": "scratch:/tmp/…", "path": "/tmp/…"},
  "requires": [{"requirement": "cc", "met": true, "detail": "cc -> /usr/bin/cc"}],
  "steps": [{
    "id": "short-write-append",
    "fault": {"id": "S-004", "name": "short-write", "backend": "shim", "tier": "L1", "params": {"…": "…"}},
    "injected_at": "…",
    "landed": true,
    "landed_witness": {"kind": "external", "source": "…", "evidence": "…"},
    "probe": {"name": "payload-complete", "expected": "…", "observed": "…", "passed": true},
    "detached": {"kind": "external", "source": "…", "evidence": "…"},
    "outcome": "passed",
    "duration_ms": 412
  }],
  "outcome": "passed",
  "notes": ["…"],
  "started_at": "…", "finished_at": "…",
  "reproduce": "mischief profile run storage/short-write --target scratch:/tmp/… --seed 7 --json"
}
```

Fields worth reading first:

- `landed` + `landed_witness` — the fault actually reached the target. A witness
  whose `kind` is `injector` is the injector's own bookkeeping: corroboration,
  not proof.
- `probe.expected` / `probe.observed` — the measurement pair. If `observed` does
  not satisfy `expected`, the step is `failed` and carries `cause_class` +
  `hint`.
- `detached` — the descriptor's inverse was measured ("next write applies all
  bytes"). An unproven inverse cannot grade `passed`.
- `notes` — this build's honest disclosures (see below).
- `reproduce` — the exact command to re-run the same profile, seed and target.

## Outcome and exit codes

| outcome | exit | meaning |
|---|---|---|
| `passed` | 0 | the fault landed **and** the probe held. |
| `failed` | 1 | the fault landed **and** the probe broke. |
| `inconclusive` | 3 | no signal: missing primitive, unmet requirement, no actuator, unproven landing, or a target this build cannot attach to. |
| (refused) | 2 | usage error, unsanctioned host, protected target, missing profile — nothing was landed. |

A run's outcome is the worst of its steps: any `failed` ⇒ `failed`; else any
`inconclusive` ⇒ `inconclusive`; else `passed`. A profile that graded nothing is
`inconclusive` — an empty result is never a green.

## Run dir

`<dir>/<run_id>/` (default `<dir>` = `~/.mischief/runs/profile`) holds:

```
profile.jsonl   the journal record set (run_planned, per-step fault_armed /
                fault_landed / probe_sample / assertion_result / finding,
                verdict, run_closed) in the internal/journal format
finding.json    the exact finding document printed to stdout
shim/           the generated injection source, the built shim, the scenario
                writer and the shim's own hit counter — the injection log
```

## Deterministic re-runs (PROF-031)

A run is a pure function of its inputs: the same profile bytes + the same
`--seed` write a **byte-identical journal** (and identical generated shim
source, writer and counter), no matter when or into which `--dir` root the
run happens. Everything the journal carries is therefore either profile
order, a measured value, or derived from the seed:

- a record's `ts` is a LOGICAL, seed-derived clock (`ts = base + seq`, base
  hashed from the run id and the seed — disclosed in `run_planned`'s
  `journal_clock` field), never the wall clock. The run's wall-clock bounds
  deliberately live in `finding.json` (`started_at` / `finished_at`,
  `injected_at` per step), which is not part of the byte-identity contract;
- nothing in the executor iterates a map where order is observable: step
  params are walked sorted, steps run in profile order.

A different `--seed` changes the run id, the generated payload bytes and the
clock base — same seed, different result is then a REAL difference (the
target's own behavior changed), which is exactly what makes a FAIL->PASS
flip meaningful. Note that a run dir is content-addressed by profile bytes +
seed, so a re-run into the same `--dir` REPLACES the previous run's journal:
keep the before-run and the after-run apart with two `--dir` roots.

## Compare (`mischief profile compare`)

```sh
mischief profile run  storage/short-write --target scratch:/tmp/t --json --seed 7 --dir .runs/before
# ... fix the code ...
mischief profile run  storage/short-write --target scratch:/tmp/t --json --seed 7 --dir .runs/after
mischief profile compare .runs/before/<run_id> .runs/after/<run_id>
```

`compare` reads the two journals, pairs steps by id between the two runs of
the same profile (a different profile than run A refuses with a named
reason), and prints one line per step:

- `failed->passed` — the probe that broke in the before-run now holds: the
  fix landed;
- `passed->failed` — the before-run held and the after-run broke: a
  regression;
- `unchanged (<outcome>)` — the step graded the same in both runs;
- any other pair of the three-value outcomes — e.g. `passed->inconclusive` —
  and `not-graded` for a step a killed run never journaled a grading for.

`--json` emits one document (`mischief.compare/v1`) with the paired steps,
their `from`/`to` outcomes, transitions and counts.

## What this build does not promise

Declared here so a reader is not misled by a green finding:

- **Two actuators.** `S-004` (short write, `scratch:<path>` targets) and
  `S-010` (fsync lie, `dbdurability:<fsync|none>:<path>` targets) are the only
  primitives this build can inject. A step naming any other catalog primitive
  grades `inconclusive`, naming the missing actuator; an id absent from the
  catalog grades `inconclusive`, naming the catalog it was resolved against.
- **No live-process attachment.** The actuators fault a CHILD the executor
  spawns (the S-004 scenario writer, or the S-010 controlled commit-log
  database). Only `scratch:<path>` and `dbdurability:<fsync|none>:<path>`
  selectors are attachable; any other selector grades `inconclusive`.
- **The scenario writer is mischief's own.** At L0 the target is a plain file
  (S-004) or a controlled commit-log database the executor builds (S-010), so
  the executor supplies the reference target and the probe grades what it
  did. The shipped profiles' expected outcomes are therefore `passed` on the
  correct disciplines; the `failed` arm is covered by the executor's unit
  tests and by the S-010 profile's own no-fsync discipline (run the same
  profile against `dbdurability:none:<path>` and it grades `failed` with
  `durability:storage-ack-not-durable`), not by the shipped fsync scenario.
- **The scope ladder, the AC-19 selftest rail and the AC-8 load gate are not
  enforced by `profile run`.** The selftest harness lands the same primitives on
  L0 scratch by the same posture, and `S-004` declares tier L1 while its
  selftest is not green — the two rails are mutually unsatisfiable for every
  shipped shim primitive. The finding reports the declared tier in `notes`
  instead of implying it was met.
