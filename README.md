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
| `docs/FAULT-CATALOG.md` | 104 primitives across 8 layers (P 14 · S 14 · R 12 · F 16 · N 18 · C 12 · I 14 · T 4), each with its landed-proof and inverse, plus 9 simulated cases covering the layer-I cloud shapes — 113 rows total |
| `docs/SPEC-PLAN.md` | the spec set and the spec→PRD→AC map |
| `probe/RESULTS.md` | the measured capability audit the design rests on, with the scripts that produced it |
| `docs/EXPERIMENTS.md` | the rehearsal contract for `catalog/experiments/`: which experiments are runnable today, why the rest are rehearsal-only, and the scratch-host rule |
| `cmd/mischief/` + `Makefile` | the M1 chassis CLI (`make bin`) — core verbs: `plan` (zero side effects, AC-1: nothing is written; `--journal-dir D` opts in to recording the preview as `D/plan.jsonl`) · `status` (default: ACTIVE holds; `--all` also lists terminal `reverted` / `revert_failed` holds) · `revert` (`--all` is the kill switch; `--hold <id> --retry` re-runs a terminal `revert_failed` hold's recorded inverse as one more measured attempt) · `doctor` (rails self-checks) · `selftest --all \| --primitive <id>` (L0 scratch land+revert proofs per primitive; exit 0 only green-or-skip — the M1 exit criterion, AC-19/AC-3/AC-4) · `serve` (reverter daemon: TTLs, boot reconcile, `health.json`) · `version` (stamped git sha); v0.1 also ships `install` (privileged helper manifest + a visudo-validated sudoers drop-in — never elevates itself) · `uninstall` · `audit` (survey the live privileged surface against the canonical verb table) · `retention` (rotate old run dirs; report-only without `--apply`), plus the evidence verbs `battery` · `chaos` · `sim` · `plane` |
| `catalog/` | machine-readable fault and experiment examples |

## Install

### Option A — release tarball (no Go toolchain needed)

Short version: download, verify, unpack, then `mkdir -p ~/.local/bin && install -m 0755 mischief ~/.local/bin/`.

Releases live at <https://github.com/trouble-agent/mischief/releases>. Download the
tarball for your architecture (`linux_amd64` / `linux_arm64`) plus `checksums.txt`,
verify, and unpack (example for tag `v0.1.0`, amd64):

```sh
gh release download v0.1.0 -R trouble-agent/mischief
# or, without gh:
curl -LO https://github.com/trouble-agent/mischief/releases/download/v0.1.0/mischief_v0.1.0_linux_amd64.tar.gz

grep "mischief_v0.1.0_linux_amd64.tar.gz" checksums.txt | sha256sum -c -   # verify the archive you downloaded — checksums.txt also lists the arm64 tarball, which a bare `sha256sum -c` would report as FAILED (you never downloaded it)
tar -xzf mischief_v0.1.0_linux_amd64.tar.gz
```

The archive contains four files:

| file | what |
|---|---|
| `mischief` | the CLI — static, CGO-free binary |
| `libfault-anchored.so` | the LD_PRELOAD fault shim (mode 0755) |
| `LICENSE`, `README.md` | the usual |

Put the binary on your PATH (create the directory first if it doesn't exist):

```sh
mkdir -p ~/.local/bin && install -m 0755 mischief ~/.local/bin/
```

Then check the install:

```sh
mischief version   # mischief v0.1.0 (a release stamps the tag; `make bin` stamps the git sha)
mischief status    # folds the run dir's journal: armed/stuck holds + TTL remaining, proofs
mischief doctor    # capability/tier ladder report (see the catalog note below)
```

**Run an experiment end-to-end (plan).** From a checkout, `plan` is the
zero-side-effects preview of an experiment: it resolves the target and every
fault against the catalog and prints what would land, what proves it landed,
and how it is reverted — without touching anything. It writes no run dir and
no journal: AC-1's proof is exactly that (`plan` leaves the journal file
absent), and the run dir and the journal belong to the verbs that land and
hold. An operator who wants the preview on disk asks for it — `plan
--journal-dir D` appends one `run_planned` record (the `run_id` the render
printed, the experiment, the resolved fault set, the target and the refusal
tally) to `D/plan.jsonl`, the same per-verb journal convention
`selftest.jsonl` and `battery.jsonl` follow. Run on an unsanctioned host,
though, and the verb refuses: mischief only runs on a **sanctioned host**
(SPEC-13). On a fresh machine the gate refuses with `host_not_sanctioned`
until you export `MISCHIEF_SANCTION=1` (the env half of the sanction marker;
the file half is `/etc/mischief-sanction`, overridable with
`MISCHIEF_SANCTION_FILE` — see the Safety section). Of the shipped
experiments, exactly one is plan-clean by default:

```sh
MISCHIEF_SANCTION=1 bin/mischief plan -f catalog/experiments/ex-001-noop-detector.yaml
```

This previews S-1 ("the false green"): the injector finds no live target
holding the state file, so the fault never lands — the `run` verb that grades
it `no_op` is not implemented in v0.1 (see the verb table in the PRD). `plan`
lands no fault and writes nothing at all unless `--journal-dir D` asks for the
preview record. The other three shipped
experiments (`ex-002`, `ex-003`, `ex-004`) are rehearsal-only: they need
capabilities or selectors not yet in the catalog (`I-002`, `P-006`), or an
explicit sanction override for `F-009`, and their YAML headers say why — read
them before adapting any of them. And per SPEC-13 the sanctioned host for a
*real* run is an ephemeral scratch instance, never your daily driver.

#### First sanctioned run (the L1 container recipe)

The marker recipe above assumes the host's `/etc` is writable — on an L1
container (a bunker agent, any unprivileged sandbox) it is not, and writing
the marker there fails. The working recipe on such a host puts the
reason-bearing marker in your home directory and points the gate at it with
`MISCHIEF_SANCTION_FILE` (this is exactly what the test plane provisions —
see `docs/TEST-PLANE.md`, and `mischief doctor --self-check` names this
route itself when it finds `/etc` unwritable):

```sh
printf "%s\n" "sanctioned: ephemeral L1 container for mischief selftest" > "$HOME/mischief-sanction"
export MISCHIEF_SANCTION_FILE="$HOME/mischief-sanction"
export MISCHIEF_SANCTION=1

mischief selftest --all          # exit 0 = every primitive green-or-skip
```

The marker file must carry a reason line (an empty or comment-only file is
still a refusal — SPEC-13 fails closed), and `MISCHIEF_SANCTION_FILE` must
be exported in every shell that runs a fault-landing verb, not just the
first one.

**About the shim `.so` — where mischief actually looks.**** The tarball ships a prebuilt
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

Prove the primitives on your host before using it: `bin/mischief selftest --all`.
On a sanctioned host (SPEC-13 marker present) the contract is exit 0 =
green-or-skip; on an unsanctioned host selftest refuses fail-closed with
`host_not_sanctioned` and exit 2 — nothing lands (see the Safety section / SPEC-13;
on an L1 container where `/etc` is not writable, use the "First sanctioned run"
recipe above).

### The privileged surface: `mischief install`, `mischief audit`, `mischief uninstall`

The binary install above puts the CLI on your PATH; the privileged surface is a
separate, explicit step. `mischief install` writes the helper manifest under the
prefix (default `/var/lib/mischief`) and a sudoers drop-in at
`/etc/sudoers.d/mischief`, rendered from the canonical verb table, validated with
`visudo` before it lands, and never granting anything beyond that table. It never
elevates itself: an unprivileged caller plans the install (`--dry-run`, or
`--no-drop-in` for the manifest-only plan) but the drop-in write itself needs
root. `mischief uninstall` removes what install wrote (artefacts by default,
run-dir rotation via `--keep-count` / `--keep-days`); both verbs accept
`--dry-run` and print the exact plan before touching anything.

```sh
sudo mischief install            # manifest + visudo-validated drop-in
sudo mischief install --dry-run  # the exact plan; touches nothing
mischief audit                   # survey the live privileged surface
sudo mischief uninstall --dry-run
```

`mischief audit` grades the host's install condition, and its exit code is a
contract:

| exit | `condition` | meaning |
|---|---|---|
| 0 | `not_installed` | drop-in simply absent — a healthy pre-install state, not drift |
| 0 | `manifest_only` | manifest installed (`mischief install --no-drop-in`), drop-in absent — the drop-in write needs root; complete with `sudo mischief install` |
| 0 | `ok` | installed and byte-identical to the canonical verb table |
| 1 | `drift` | present but divergent (hand-edited or stale) — regenerate with `mischief install` |
| 1 | `unreadable` | present but unmeasurable — investigate access/permissions |

A setuid binary under the prefix is reported as a finding at any condition (the
design ships none) and also exits 1. `audit --json` carries the same facts —
including `condition` — for an operator diff; an uninstalled host is a normal
`not_installed` result, so tooling can distinguish "nothing here yet" (exit 0)
from "someone changed what is here" (exit 1). A manifest-only install
(`mischief install --no-drop-in` — the mode available without root) reports
`manifest_only` (MISCHIEF-DF-35), so tooling can equally tell it apart from a
never-installed host.

## The loop

```
declare → plan → arm → LAND → prove landed → hold → observe → revert → prove reverted → grade → journal → findings
```

## Arming a hold (`mischief arm`)

The `arm` verb is the write-ahead half of the loop (SPEC-04): it records a
reverter hold — fault id, target, the inverse action, the measured
post-revert check, and a TTL — into the run dir's journal, fsynced BEFORE the
fault may land. The serve daemon (or a detached TTL owner) reverts the hold
at TTL expiry from the journal bytes alone; `mischief revert --all` is the
kill switch.

```sh
mkdir -p /tmp/mischief-scratch
echo 'original' > /tmp/mischief-scratch/config.toml   # the file the fault will target — create it before backing it up
cp /tmp/mischief-scratch/config.toml /tmp/mischief-scratch/config.toml.bak
bin/mischief arm \
  --fault S-001 --target scratch:/tmp/mischief-scratch \
  --ttl 30s \
  --inverse-kind restore-file \
  --inverse-params path=/tmp/mischief-scratch/config.toml,backup=/tmp/mischief-scratch/config.toml.bak \
  --check-kind file-matches-backup \
  --check-params path=/tmp/mischief-scratch/config.toml,backup=/tmp/mischief-scratch/config.toml.bak \
  --dir ~/.mischief/runs/default

bin/mischief status --dir ~/.mischief/runs/default   # shows the armed hold + TTL remaining
bin/mischief serve --dir ~/.mischief/runs/default    # watchdog reverts at TTL, writes health.json
```

Decl kinds are the reverter's closed vocabulary (fail closed on unknown
kinds): inverse — `restore-file` (params `path`,`backup`) | `remove-file`
(param `path`); check — `file-matches-backup` (params `path`,`backup`) |
`file-absent` (param `path`). Like the fault-landing verbs, `arm` runs the
SPEC-13 sanction gate and refuses fail-closed (exit 2, nothing written) on an
unsanctioned host. Arming writes the hold only — landing the fault remains
the fault verbs' act.

## Landing the fault, and recovering a failed revert

`arm` is write-ahead only: it records the hold in the journal and stops there.
**Landing the fault is a separate act**, performed by the operator or by the
fault verbs (`sim`, `selftest`, `battery`, `plane`) — the bare `LAND` step in
the loop above. Landing means making the fault real on the target yourself;
for a file-scoped fault that is the change the fault models:

```sh
printf 'FAULTED' > /tmp/mischief-scratch/config.toml   # the LAND step
bin/mischief status --dir ~/.mischief/runs/default      # the hold is still armed
```

The hold's measured check and its recorded reverse action decide the outcome:
the serve daemon (or a detached TTL owner) reverts at TTL expiry, and
`mischief revert --all` is the kill switch. The full contract — what to write,
what proof lines to expect — is spelled out in `skills/mischief-usage/`
(`# LAND the fault yourself (arm writes the hold only; printf FAULTED > file, etc.)`).

Two operator flags close the surfaces around a failed revert. They are the
recovery path, so they are worth knowing before you need them:

| command | what it does |
|---|---|
| `mischief status --all` | lists TERMINAL holds (`reverted` / `revert_failed`) alongside active ones. The default `status` shows active holds only, so a hold that has already ended is visible here — and only here. |
| `mischief revert --hold <id> --retry` | re-executes a terminal `revert_failed` hold's recorded inverse as one more MEASURED attempt (same inverse, same check, same grading); exit 0 only if the retry measures `reverted`. A plain `revert --hold <id>` refuses a terminal `revert_failed` hold, so this flag is the only way out of that dead end. `--retry` and `--all` are mutually exclusive. |

## Safety, in one breath

No setuid. Rootless-first (`systemd-run --user` scopes, LD_PRELOAD, seccomp, userspace
proxy), a narrow `sudo` allowlist for host-scoped faults, TTL + detached reverter, boot
reconcile, one kill switch (`mischief revert --all`), a load gate, and a protected-target
list that keeps the fleet's control planes out of the automatic path.

Isolation contract (SPEC-13): mischief's own runs — development, selftest, battery —
require an explicit **sanction marker** (file + env) and execute on an **ephemeral
sanctioned host** (a bunker instance), never on the fleet's main host. An unsanctioned
host, including the main fleet host, refuses fail-closed at target resolution (see the
PRD, "Where mischief ITSELF is allowed to run"). On an L1 container `/etc` is not
writable — the sanctioned-run recipe there is a reason-bearing marker at
`$HOME/mischief-sanction` plus `MISCHIEF_SANCTION_FILE` pointing at it (see the
"First sanctioned run" subsection under Install).

## License

MIT — see `LICENSE`.
