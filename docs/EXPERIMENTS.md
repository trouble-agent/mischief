# Experiments

The four replay experiments under `catalog/experiments/` are rehearsal material,
not run books. Each YAML carries a header block that states its status; this
page is the contract that binds all four. Nothing on this page overrides a
header — read both before touching a target.

## Status of each experiment

| file | runnable today | why |
|---|---|---|
| `catalog/experiments/ex-001-noop-detector.yaml` | yes | plans clean today — measured: `mischief plan` exits 0, the target selector `host:.` resolves and no rail refuses; still scratch-only, and still read its header first — the fault truncates a live writer's state file |
| `catalog/experiments/ex-002-redis-loss-recovery.yaml` | no, rehearsal-only | measured refusal: `mischief plan` exits 2 with `scope_below_minimum` — the selector `container:trouble-hub` resolves, but I-002's declared minimum tier is L3/L4 and a scratch-host plan sits at L0; independently of the tier gate, the hardcoded `127.0.0.1:7699` / `7661` probes are placeholders, and the inverse restore carries no state-backup assertion (see below) |
| `catalog/experiments/ex-003-spool-kill-between.yaml` | no, rehearsal-only | measured refusal: `mischief plan` exits 2 with unknown primitive P-006 (P-006 is not in the loaded catalog) plus `scope_below_minimum` — the selector `process:troubled` resolves; the `<state_root>` probes remain hand-substituted placeholders |
| `catalog/experiments/ex-004-live-file-replace.yaml` | no, rehearsal-only | measured refusal: `mischief plan` exits 2 with `protected_target` — the resolver refuses the gateway as fleet control plane (an operator override is possible but is then recorded); replacing a live `state.db` under a running process is data destruction on anything but a scratch copy |

## The ex-002 backup precondition

ex-002 rehearses a live redis-loss recovery with a `mode: fresh-empty` fault and
an inverse `mode: restore` that asserts nothing: the YAML nowhere proves a
backup existed or that the restore actually brought the state back. On fleet
hosts live stacks answer adjacent ports to the hardcoded `127.0.0.1:7699` /
`7661` probe endpoints, so an operator following the YAML verbatim against a
real target would wipe live state (the I-002 shape). Before any real run,
ex-002 must first gain a state-backup assertion: take and verify a backup of the
target state before arming the fault, and assert after the inverse that the
state was actually restored.

## Placeholders are literals

`<state_root>`, `<key>`, `<stream>`, `<group>`, `<date>`, `<app>`, `<oracle>`
and every other angle-bracketed token in the experiment YAMLs are placeholder
literals. Nothing fills them in: not the loader, not the planner, not the docs.
They mark values that must be substituted BY HAND, for a specific scratch
target, by the operator who runs the experiment. The placeholders are not
what stops a rehearsal file from planning clean: target selectors resolve
today (SPEC-03 has landed — the grammar is the section below), and the
measured refusals in the table above come from the tier scope ladder, the
loaded catalog and the protected-target list, never from the placeholders.

## Target-selector grammar (SPEC-03)

The `target:` selector in every experiment YAML is `<kind>:<selector>`: a kind
prefix, a literal colon, and a selector string whose meaning depends on the
kind. The kinds shipped in `internal/rails/targets.go` (`TargetKind`) are
exactly:

| kind | selector form | example |
|---|---|---|
| `process` | a decimal PID or the component name of a fleet component (`pid:<n>` also accepted) | `process:1234`, `process:scheduler` |
| `cgroup` | an absolute cgroup-v2 path | `cgroup:/users.slice/...` |
| `container` | a container name | `container:trouble-hub` |
| `host` | a host-rooted component name (`host:` with no suffix is also accepted) | `host:memory-daemon`, `host:` |

A bare `<kind>:<selector>` with no colon is not a target; whitespace around
the selector is trimmed; a selector that names nothing on the box is refused
at resolution (exit 2, never a best guess). The resolver — `rails.Resolve` in
`internal/rails/targets.go` — applies its rails in doctrine order BEFORE any
actuator is chosen, so naming a different primitive cannot route around a
refusal: declaring no target at all is refused with exit 2 and nothing landed
(AC-2); PID 1, kernel threads, and mischief's own tree or executable are
structurally excluded with no override at all; the scheduler, the gateway and
the memory daemon are refused by default as protected targets, recorded if an
operator override is explicitly given (AC-7). A protected target cannot be
reached by picking a different kind: the protected list is matched on the
selector's canonical form or any component (`process:scheduler`,
`container:gateway`, `host:memory-daemon` are all the scheduler, the gateway
and the memory daemon to the resolver).

What SPEC-03 does not decide here: WHICH experiment is runnable today is the
table above; the resolver exists in `internal/rails/` but the experiment
runner that would call it does not exist yet, so nothing on this page runs
experiments automatically.

## The contract

A rehearsal YAML is never an authorization to run outside an L0 scratch host.
Runnable-today status in the table above is the measured plan verdict: it means
`mischief plan` exits 0 for that file today, and every exit-2 refusal is
recorded verbatim in the why column; it does not mean "safe on a fleet host".
Every experiment, including ex-001, runs on a scratch host or not at all.

---
ch:trace row=MISCHIEF-DF-64 spec=docs/EXPERIMENTS.md#runnable-today wave=.coding-hermes/waves/mischief-foreman-2026-10-21-17-15-43.json#step-2 test=none:docs-only doc=docs/EXPERIMENTS.md prompt=/tmp/mischief-foreman-2026-10-21-17-15-43-MISCHIEF-DF-64-brief.txt evidence=docs/EXPERIMENTS.md witness=none:no external witness required verdict=none:foreman-runs-judge commit=none:pending-foreman-trace memory=none:not-durable-yet
