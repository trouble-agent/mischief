# mischief

**Fault injection for a fleet of Linux daemons.** Declare a fault, land it in a named
target, **prove it landed**, hold it under a TTL owned by a reverting watchdog that
outlives the CLI, and get a verdict on whether the target recovered.

> **Status: ACTIVE DEVELOPMENT.** Name and repo home are decided (MSF-001): `mischief`, at
> [`trouble-agent/mischief`](https://github.com/trouble-agent/mischief), private-first. Sibling to
> [`trouble`](https://github.com/trouble-agent/trouble) — `trouble` detects and remediates,
> `mischief` induces and grades. SPEC-01 has shipped: a tested fault catalog lives under
> [`internal/catalog/`](internal/catalog/) (~1,600 lines of Go: schema types, loader, tests), with
> the spec under [`docs/`](docs/).

## Why

The fleet's only fault-injection surface is five bash cells in one QA harness, and the
harness's own comments record that four of them have graded the harness rather than the
product (a corruption cell that never started the app, a disconnect verdict shorter than
the suite, a resource cell measuring a 3 GB cap, a shutdown cell measuring a missing
docker CLI). None of them can tell *"the fault landed and survived"* from *"the fault
never landed"* — the most expensive false green in this class. Meanwhile the fleet's real
recovery bugs (an unreachable Redis rewire, a queue "durable in name only", a two-day
archive hole behind an untested restore path) were found by hand or not at all.

## What it is

- **Faults are data** with three obligations: an **inverse**, a **landed-proof**, and an
  enumerable **blast**. The catalog is content; the backends are code.
- **Prove it landed, or it is a `no_op`.** A run that cannot show the fault arrived fails.
- **Revert is measured, not asserted**, and the reverter does not die with the CLI.
- **Verdicts are closed and honest**: `recovered`, `degraded`, `not_recovered`, `hung`,
  `corrupted`, `no_op`, `aborted`, `flaky`, `void`.
- **No default target, ever**; a global armed-fault set is a documented wrong turn.
- Targets: process · cgroup · container · host · simulated cloud resource (real-provider
  adapter is opt-in, allowlisted and spend-capped; permanent destruction is
  simulator-only).

## Docs

| file | what |
|---|---|
| `docs/prd/mischief-v0.1.md` | the PRD (front door): problem, user stories, design, safety, ACs, replays, open decisions |
| `docs/FAULT-CATALOG.md` | 104 primitives across 8 layers (P 14 · S 14 · R 12 · F 16 · N 18 · C 12 · I 14 · T 4), each with its landed-proof and inverse |
| `docs/SPEC-PLAN.md` | the spec set and the spec→PRD→AC map |
| `probe/RESULTS.md` | the measured capability audit the design rests on, with the scripts that produced it |
| `catalog/` | machine-readable fault and experiment examples |

## The loop

```
declare → plan → arm → LAND → prove landed → hold → observe → revert → prove reverted → grade → journal → findings
```

## Safety, in one breath

No setuid. Rootless-first (`systemd-run --user` scopes, LD_PRELOAD, seccomp, userspace
proxy), a narrow `sudo` allowlist for host-scoped faults, TTL + detached reverter, boot
reconcile, one kill switch (`mischief revert --all`), a load gate, and a protected-target
list that keeps the fleet's control planes out of the automatic path.

## License

MIT — see `LICENSE`.
