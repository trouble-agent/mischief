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
| `probe` | yes | the probe that grades the step. Closed vocabulary: `payload-complete`. |
| `cause_class` | no | the failure class a `failed` step routes to. Must be declared in `cause_classes` (referential integrity is checked at load, so an agent never receives a hint that does not exist). |

### `requires[]`

Each entry is `token: prose` (or a bare `token`). The token comes from a closed
vocabulary and is resolved against the host at run time:

| token | met when |
|---|---|
| `cc` | a C compiler is on `PATH` (the shim is generated and built per run). |
| `scratch-target` | the target is an absolute path under the scratch base (`$TMPDIR` or `/tmp`). |
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

## What this build does not promise

Declared here so a reader is not misled by a green finding:

- **One actuator.** `S-004` (short write) is the only primitive this build can
  inject. A step naming any other catalog primitive grades `inconclusive`,
  naming the missing actuator; an id absent from the catalog grades
  `inconclusive`, naming the catalog it was resolved against.
- **No live-process attachment.** The actuator faults a CHILD the executor
  spawns. Only `scratch:<path>` targets are attachable; any other selector
  grades `inconclusive`.
- **The scenario writer is mischief's own.** At L0 the target is a plain file,
  so the executor supplies a reference writer (the POSIX-correct loop) and the
  probe grades what it landed. The shipped profile's expected outcome is
  therefore `passed`; the `failed` arm is covered by the executor's unit tests
  (a probe-breaking actuator), not by the shipped scenario.
- **The scope ladder, the AC-19 selftest rail and the AC-8 load gate are not
  enforced by `profile run`.** The selftest harness lands the same primitives on
  L0 scratch by the same posture, and `S-004` declares tier L1 while its
  selftest is not green — the two rails are mutually unsatisfiable for every
  shipped shim primitive. The finding reports the declared tier in `notes`
  instead of implying it was met.
