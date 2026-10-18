# mischief — the full-matrix + filing surface, driven for the first time (dogfood 2026-10-18)

Lane: `mischief-chaos-dogfood` · project `mischief` · product HEAD **2d1ed37** (`2d1ed37b5cdf`,
product code unchanged since `c28800d`, 2026-10-17) · bin built by this run from a clean `git archive`
export (`make bin`, 2.4s warm) · live legs on an **ephemeral sanctioned bunker agent** `f306b7bd`
(bunker-mvp, 1h TTL, destroyed at the end — `bunker list` empty, `Residue: 0 orphan users, 0 orphan
homes`).

## Why this surface

Eighteen prior dogfood ticks drove `battery --quick`, `selftest`, `chaos`, `sim`, `plane`, the
lifecycle verbs and the serve/watchdog pair. The **full-matrix + FILING path** —
`battery --project <p> --file-rows`, which `docs/CHAOS-LANE.md` §6 step 3 names as the chaos lane's
tick body and `docs/BATTERY-PARITY.md` names as the shipped replacement for the five legacy bash
cells — had **never been driven**: the whole dogfood log carries zero hits for `file-rows` or
`battery --project`.

## What was actually run (all on the sanctioned agent, all `rc=0`)

| command | wall | result |
|---|---|---|
| `battery --dry-run --project mischief` | 27ms (10 warm runs 18–28ms, mean 21.7) | 11 cells enumerated, nothing written (`--out` dir never created) |
| `battery --project mischief --file-rows` (COLD) | **911ms** | run `2f5f4eef999c04c9` · 5 pass / 6 named skips / 0 fail · `findings.jsonl` written empty · `matrix.json` + `matrix.md` · journal 13 records (landed=5, reverted=5) |
| same command again (WARM, fresh run dir) | **816ms** | run `31b2dc9074d1c370` — identical outcome (reproducible) |
| `battery --primitive N-012 --file-rows` (the documented release-gate cell) | 28ms | run `fedb6e7e59fd9979`, 1 pass |
| `battery --quick --file-rows` (parity matrix) | 586ms | run `aaf4de007edb5f29`, q1–q5 with `[replaces chaos-*]` annotations, 4 pass / 1 skip |

The six skips are honest, per-cell, capability-named (`an L3 container actuator`, `no local image`,
`an L2-scoped namespace target`, `shim short-write mode`, `shim clock interception`, `no measured
selftest actuator`). The five executed cells carry real landed proofs in the journal — inode
identity, proxy hit-counter, `/proc` state, systemd scope read-back, shim counter — and matching
revert proofs. **The surface works.** The findings below are about what it emits and what it hides.

## Findings (rows filed this tick)

| row | P | one line |
|---|---|---|
| MISCHIEF-DF-43 | P2 | the ids both filing paths emit (`MSF-BAT-<project>/<PRIM>` · `MSF-CHAOS-<lowercase hex>`) are refused by `boardctl update`/`create` (`^[A-Z][A-Z0-9]+(-[A-Z0-9]+)+$`), while `boardctl validate` — the AC-18 criterion — passes them |
| MISCHIEF-DF-44 | P3 | the matrix's detection half is `not_wired` on **11/11** cells in the live artefact; no production path can wire it; MSF-011 is closed *complete* with "AC-14 (detection assertion against trouble's ledger)" in its acceptance criteria, and BATTERY-PARITY sells the artefact as *fault × detection × recovery* |
| MISCHIEF-DF-45 | P4 | `battery`'s flag help documents `default: every catalog primitive` / `default: scratch`; the bare verb refuses `rc=2` "nothing to run" — neither documented default exists |
| MISCHIEF-DF-46 | P4 | no per-cell timing survives any artefact (all 13 journal records share one 1-second `ts`, no duration field, nothing in the CLI or matrix), so "which cell was slow" is unanswerable |

Events 135–138 (`task_created` for each row) are in `board/events.jsonl`; `boardctl validate` =
`RESULT: OK`.

## Discriminating probes behind the rows

**DF-43.** The id law was tested with the fleet's own tooling against scratch boards, one row each:
`boardctl update "MSF-BAT-mischief/F-009" --status complete` → `rc=1` *"does not match the fleet id
format ^[A-Z][A-Z0-9]+(-[A-Z0-9]+)+$"*; `boardctl create --id "MSF-BAT-mischief/C-009"` → `rc=1`;
`boardctl update "MSF-CHAOS-2dfb1056d701"` (the id `chaos finding` really emitted, `rc=0`) →
`rc=1`; control `MSF-BAT-MISCHIEF-F-009` → `rc=0 updated`. `boardctl validate` returns OK on both
boards, which is exactly why the AC-18 criterion never caught it. The battery half is a
template-plus-observed-input claim (the adverse branch that writes the row is unreachable on a
healthy host — see the coverage note below); the chaos half is a captured emission.

**DF-44.** Code census on the clean export: `internal/battery/runner.go:24` ("The v0.1 build wires NO
real detector"), the `not_wired` constant at `:56`, the `Detectors` map at `:288-290`, and
`cmd/mischief/battery.go:126` ("Detectors stay unwired") with no construction site anywhere;
`grep -rn 'internal/observe' --include='*.go'` finds **no non-test importer** — `internal/observe`
(observe.go, gap.go, verifyplay.go) is reached only by its own tests, whose `ch:trace` witness reads
`none:no-live-trouble-instance-in-worktree`.

**DF-45.** Same refusal captured twice — on the fleet host (unsanctioned: the SPEC-13 refusal, which
is correct) and on the sanctioned agent, where `--project mischief` returns `rc=0` in the same
session. So the "nothing to run" refusal is host-independent and contradicts the flag help.

**DF-46.** The pulled journal (3 768 bytes, base64 through `bunker exec`, sha-checked by byte count)
has record types `run_planned · fault_landed×5 · fault_reverted×5 · verdict · run_closed`, every one
with `ts: 1792306814` — the whole 911ms run sits inside one second.

## Isolation & install leg (not skipped)

- **SPEC-13 held on the scheduler's own host**: `battery --primitive N-012` there → `rc=2`
  `host_not_sanctioned ... nothing was written`, with no sanction marker present (8ms).
- **MISCHIEF-DF-18 (closed, P1) re-proved on fresh evidence.** A lane YAML declaring
  `host.name: karaHermes-mde-7840hs` (this machine's real hostname) → `chaos check` `rc=2` *"is a
  forbidden host ... matched this machine's resolved hostname, not just the placeholder name"*;
  control with another hostname → `rc=0` + board-symlink evidence. The fix holds.
- **Install leg = fresh machine, not inherited.** `scripts/bunker-plane.sh run --ttl=1h` on
  bunker-mvp: preflight / build (`CGO_ENABLED=0 linux/amd64` from frozen HEAD 2d1ed37b5cdf, 12 110 475
  bytes) / spawn (`f306b7bd`, exec gate passed) / ship ×2 (repo tar 766 176 B + binary sha256-verified)
  / selftest (`selftest: 13 pass, 9 skip, 0 fail (of 22)`) / collect (journal 38 lines sha256-verified,
  landed=13 reverted=13) — **all pass**; the plane was run with `BUNKER_PLANE_KEEP=1` so the same
  agent could carry this run's battery legs, and **this run destroyed it afterwards**:
  `bunker destroy f306b7bd` → "Agent f306b7bd destroyed", `bunker list` → "No agents found".

## Coverage note (honest gap, no row)

The **adverse branch of `--file-rows`** — the one that writes a non-empty `findings.jsonl` — could not
be reached from a healthy sanctioned host: every executed cell graded `recovered`, so all three
`findings.jsonl` files are 0 bytes and the CLI printed `findings: none`. Attempts to force an adverse
verdict with a bounded, self-scoped squeeze (`ulimit -v 512M / 256M`, the legacy `chaos-resource`
shape) died in the Go runtime before any cell ran (`fatal error: failed to reserve page summary
memory`, `rc=2`) — a probe limitation, not a product defect, and it is *not* filed as one. The
emission code path is therefore proven only by its green arms; `MISCHIEF-DF-43` says so explicitly
rather than implying a captured row.

## Performance (Step 2b) — no PERF row, deliberately

Everything measured is far below what a user would wait on: full matrix + filing **0.911s cold /
0.816s warm** for 11 cells (5 executed), `--quick` 0.586s, one cell 0.028s, matrix resolution
0.018–0.028s over repeated warm runs (agent: 8 vCPU, loadavg 1.41). Profiling was not justified;
`MISCHIEF-DF-46` is about the missing *per-cell number*, not about a slow path.

## Lane hygiene

Read-only with respect to product code and board state: no source file touched, no pre-existing row
edited, no row re-filed, no foreman woken, no scheduler state touched, repo visibility/permissions
untouched. Every probe lived under `/tmp/dfmc-r19` (host) or `$HOME/mc-r19` (agent, destroyed with
the agent). The repo's uncommitted DF-39 WIP (`internal/daemon/daemon.go`, `internal/reverter/journal.go`
+ two test files, mtime 2026-10-17 09:10) was left exactly as found; this run's binary came from a
`git archive HEAD` export, never from that dirty tree.

Observation, not a row: `bunker status` on bunker-mvp reports `19 orphan keys` (0 orphan users/homes)
— key residue from earlier ephemeral agents that the plane's destroy leg (which re-checks the agent
*list*) does not cover. It predates this run and cannot be attributed to it, so it is logged here for
the chaos/plane owner rather than filed as a defect.
