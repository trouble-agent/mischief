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
| `docs/EXPERIMENTS.md` | the rehearsal contract for `catalog/experiments/`: which experiments are runnable today, why the rest are rehearsal-only, and the scratch-host rule |
| `cmd/mischief/` + `Makefile` | the M1 chassis CLI (`make bin`): `plan` (zero side effects, AC-1) · `status` · `revert` · `doctor` (rails self-checks) · `selftest --all \| --primitive <id>` (L0 scratch land+revert proofs per primitive; exit 0 only green-or-skip — the M1 exit criterion, AC-19/AC-3/AC-4) · `serve` (reverter daemon: TTLs, boot reconcile, `health.json`) · `version` (stamped git sha) |
| `catalog/` | machine-readable fault and experiment examples |

## Install

### Option A — release tarball (no Go toolchain needed)

Releases live at <https://github.com/trouble-agent/mischief/releases>. Download the
tarball for your architecture (`linux_amd64` / `linux_arm64`) plus `checksums.txt`,
verify, and unpack (example for tag `v0.1.0`, amd64):

```sh
gh release download v0.1.0 -R trouble-agent/mischief
# or, without gh:
curl -LO https://github.com/trouble-agent/mischief/releases/download/v0.1.0/mischief_v0.1.0_linux_amd64.tar.gz

sha256sum -c checksums.txt        # each downloaded archive must print OK
tar -xzf mischief_v0.1.0_linux_amd64.tar.gz
```

The archive contains four files:

| file | what |
|---|---|
| `mischief` | the CLI — static, CGO-free binary |
| `libfault-anchored.so` | the LD_PRELOAD fault shim (mode 0755) |
| `LICENSE`, `README.md` | the usual |

Put the binary on your PATH:

```sh
install -m 0755 mischief ~/.local/bin/
```

Then check the install:

```sh
mischief version   # mischief v0.1.0 (a release stamps the tag; `make bin` stamps the git sha)
mischief status    # folds the run dir's journal: armed/held holds, stuck, proofs
mischief doctor    # capability/tier ladder report (see the catalog note below)
```

**About the shim `.so` — where mischief actually looks.** The tarball ships a prebuilt
`libfault-anchored.so`, but mischief does NOT load it from beside the binary. When it
arms a shim fault it checks exactly one prebuilt location:

```
/tmp/mischief-shim/libfault-anchored.so
```

If that file is absent it compiles the shim itself, from source embedded in the binary,
with `cc -shared -fPIC -O2 … -ldl` — so a C compiler on PATH is what the shim backend
really needs. Neither prebuilt `.so` nor compiler → the run refuses with
`capability_unavailable` instead of executing the target unfaulted. If you want the
release `.so` used as-is, drop it into the one place that is checked:

```sh
mkdir -p /tmp/mischief-shim && install -m 0755 libfault-anchored.so /tmp/mischief-shim/
```

Two caveats for a tarball-only install: `mischief doctor` needs the in-repo
`catalog/faults` directory (resolved from the cwd upward) and exits 1 when it cannot
load it — run it from a checkout, or point `--catalog-dir` at one. And verbs that
actually land faults (`selftest`, `battery`) additionally require the SPEC-13 sanction
marker and a sanctioned host, so they are checkout+sanction operations, not quickstart
steps.

### Option B — build from source

```sh
git clone https://github.com/trouble-agent/mischief
cd mischief
make bin           # → bin/mischief, CGO_ENABLED=0, version stamped with the git sha
```

Prove the primitives on your host before using it: `bin/mischief selftest --all`
(exit 0 = green-or-skip).

## The loop

```
declare → plan → arm → LAND → prove landed → hold → observe → revert → prove reverted → grade → journal → findings
```

## Safety, in one breath

No setuid. Rootless-first (`systemd-run --user` scopes, LD_PRELOAD, seccomp, userspace
proxy), a narrow `sudo` allowlist for host-scoped faults, TTL + detached reverter, boot
reconcile, one kill switch (`mischief revert --all`), a load gate, and a protected-target
list that keeps the fleet's control planes out of the automatic path.

Isolation contract (SPEC-13): mischief's own runs — development, selftest, battery —
require an explicit **sanction marker** (file + env) and execute on an **ephemeral
sanctioned host** (a bunker instance), never on the fleet's main host. An unsanctioned
host, including the main fleet host, refuses fail-closed at target resolution (see the
PRD, "Where mischief ITSELF is allowed to run").

## License

MIT — see `LICENSE`.
