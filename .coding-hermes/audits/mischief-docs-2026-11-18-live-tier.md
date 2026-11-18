# mischief docs audit — live-daemon tier advance (tick mischief-perf-2026-11-18-02-23-10)

Repo: trouble-agent/mischief
Audited tip: 8b3bbc71ce16f6433c07199fbe62e51b01a7cf98 (origin/main, HEAD == origin/main, `git rev-list --left-right --count origin/main...HEAD` = 0 0)
Binary: built from the audited tip — `go build -o <scratch>/d3/mischief ./cmd/mischief` from /home/kara/mischief (BUILD_RC=0);
`<scratch>/d3/mischief version --json` -> {"version":"unknown","tool":"mischief"} (no ldflags on a plain build; the in-tree
`bin/mischief` reports e5a1c55e9f18 and is therefore STALE versus this tip — every reading below is from the freshly built binary).
Probe root: /home/kara/.hermes/cache/scratch (prunable) — this transcript is the durable artifact.
Target checkout was NOT modified: all probes ran against scratch run dirs; no target file was written by this lane.

Why this pass: the two prior deliveries of this tick (21:44, 22:02, 22:22) swept the STATIC surface (flags, env keys,
journal types, trace markers, residue) and left the live daemon unprobed ("No live daemon was started"). This pass advances
that boundary: a real `serve` was started, scraped, and driven through the documented arm -> land -> TTL-fold loop.

---

## Probe 1 — documented metrics surface, live (docs/OPERATIONS.md:68-113)

Command (documented form):

    MISCHIEFD_METRICS_ADDR=127.0.0.1:19464 <bin> serve --dir <scratch>/runs &
    curl -s http://127.0.0.1:19464/metrics | grep '^mischiefd_'

Exact output (first scrape, 1887 bytes):

    mischiefd_up 1
    mischiefd_start_time_seconds 1795008651
    mischiefd_cycles_total 1
    mischiefd_scrapes_total 1
    mischiefd_revert_total{result="no_hold"} 0
    mischiefd_revert_total{result="not_landed"} 0
    mischiefd_revert_total{result="revert_failed"} 0
    mischiefd_revert_total{result="reverted"} 0
    mischiefd_errors_total{reason="health_write"} 0
    mischiefd_errors_total{reason="refold"} 0
    mischiefd_errors_total{reason="revert"} 0
    mischiefd_revert_duration_seconds_bucket{le="0.005"} 0
    ... (0.01 0.025 0.05 0.1 0.25 0.5 1 2.5 5 10 +Inf)
    mischiefd_revert_duration_seconds_sum 0
    mischiefd_revert_duration_seconds_count 0

Verdict on the documented table (OPERATIONS.md:87-95): ACCURATE, every family and every label value present from the
first scrape — 4 `result` values (no_hold, not_landed, revert_failed, reverted), 3 `reason` values (health_write, refold,
revert), histogram buckets 0.005 .. 10 plus +Inf. No undocumented family.
Probe note (my own artifact, not a finding): a naive `grep -c '^mischiefd_revert_duration_seconds[{ ]'` returns 0 because
Prometheus suffixes a histogram's series with `_bucket`/`_sum`/`_count`; the documented histogram IS exposed.

## Probe 2 — documented 5s sweep cadence (docs/OPERATIONS.md:48-56)

    cycles_total 0 at t=+1s (sample A, unix 1795008767)
    cycles_total 2 at t=+12s (sample B, unix 1795008778)

2 cycles in ~11s matches the documented `HeartbeatInterval = 5s` (internal/daemon/daemon.go:43; IncCycle once per tick at
daemon.go:182). ACCURATE.

## Probe 3 — documented non-fatal metrics bind failure (docs/OPERATIONS.md:82-83)

Second daemon started on the SAME port with its own run dir:

    --- serve2 stdout ---
    mischiefd: run_dir=<scratch>/runs2 boot=folding health=<scratch>/runs2/health.json
    mischiefd: metrics=http://127.0.0.1:19465/metrics
    --- serve2 stderr ---
    serve: metrics: daemon: metrics listen 127.0.0.1:19465: listen tcp 127.0.0.1:19465: bind: address already in use
    --- serve2 alive? --- ALIVE
    --- serve2 wrote its health.json? --- -rw------- 291 bytes
    --- serve2 generated_at advancing? --- 1795008783

The documented log prefix appears verbatim and containment continues. ACCURATE. Documented disable values also hold:
`MISCHIEFD_METRICS_ADDR=off` and `=disabled` both suppress the `metrics=` line and leave nothing listening
(`curl` to the port -> 000); source cmd/mischief/main.go:624-628 accepts both.

## Probe 4 — documented arm -> land -> TTL-fold loop, driven through the running daemon

    MISCHIEFD_METRICS_ADDR=127.0.0.1:19466 <bin> serve --dir <scratch>/runs &
    MISCHIEF_SANCTION=1 MISCHIEF_SANCTION_FILE=<scratch>/marker <bin> arm \
      --fault S-001 --target scratch:<scratch>/target --ttl 8s \
      --inverse-kind restore-file --inverse-params path=...,backup=... \
      --check-kind file-matches-backup --check-params path=...,backup=... --dir <scratch>/runs

    armed 20253ea8b28688ea fault=S-001 target=scratch:<scratch>/target ttl=8s dir=<scratch>/runs
    write-ahead: inverse + check durable in the journal (SPEC-04); serve reverts at TTL; maximum overshoot after expiry=5s (HeartbeatInterval)

    status --dir <runs>:  holds: 1 / 20253ea8... armed ttl_remaining=8s
    (landed with the documented "land it yourself" step: printf 'FAULTED\n' > <scratch>/target/config.toml)
    wait 12s ->
    target after fold: "original"   RESTORED byte-identical (cmp -s rc=0)
    status --all:  20253ea8... not_landed  S-001 role=owner reason=ttl_expiry
                   detail=fault never landed (no landed record in the write-ahead journal); cleanup inverse ran and the
                   target was restored, but nothing was ever injected so this proves nothing about the target
    metrics after the fold:
      mischiefd_cycles_total 2
      mischiefd_revert_total{result="not_landed"} 1
      mischiefd_revert_total{result="reverted"} 0
      mischiefd_revert_duration_seconds_count 1   _sum 0.00626449   bucket{le="0.1"} 1

The TTL fold fires and restores the target byte-for-byte (documented timing holds). The `not_landed` grade is the SUBJECT OF
OPEN ROW DOC-31 ("skills/mischief-usage omits the land verb, so its documented arm->revert loop ends in a false not_landed
(rc=3)") — this probe RE-VERIFIES that premise with live output: the skill's own `printf FAULTED > file` step grades
not_landed because only the `land` verb writes the landed record the grader requires. Not filed again.

Also re-verified for MISCHIEF-DF-75 (in_progress, "mischiefd_revert_total omits result=not_landed until the first not_landed
revert"): at this tip a FRESH daemon emits `mischief_revert_total{result="not_landed"} 0` on its first scrape (Probe 1),
because internal/daemon/metrics.go:knownOutcomes now seeds from reverter.DeclaredOutcomes(). The row's premise is satisfied at
8b3bbc7 — reported as a close-out observation, not a doc finding.

## Probe 5 — health.json field reference (docs/OPERATIONS.md:60-66)  [FINDING -> DOC-38]

docs/OPERATIONS.md:65-66 says: "Key fields: `ok`, `daemon_boot`, `pid`, `run_dir`, `holds`, `stuck`, `reverted`,
`revert_failed`, `not_landed`." Live capture of a real daemon, armed hold (ttl 60s, one tick later):

    {"ok":true,"daemon_boot":"c67fcb6b0d97e031","pid":246374,"run_dir":"<scratch>/runs",
     "started_at":1795008846,"generated_at":1795008851,
     "holds":[{"hold_id":"9ea5976a83ada8ba","fault_id":"S-001","target":"scratch:<scratch>/target",
               "status":"armed","ttl_remaining_sec":56.984569128}],
     "stuck":0,"reverted":0,"revert_failed":0,"not_landed":0}
    holds[0] keys: fault_id,hold_id,status,target,ttl_remaining_sec

Live capture, terminal hold (same daemon after the fold, Probe 4):

    "holds":[{"hold_id":"20253ea8b28688ea","fault_id":"S-001","target":"scratch:<scratch>/target","status":"not_landed"}]

Source declares the exact envelope at internal/daemon/daemon.go:47-95: top-level keys ok, daemon_boot, pid, run_dir,
started_at, generated_at, holds, stuck, reverted, revert_failed, not_landed, refold_error(omitempty); holds[] element keys
hold_id, fault_id, target, status, + ttl_remaining_sec(omitempty, armed rows) + stuck_at(omitempty, stuck rows).

Undocumented in the operator page, and absent from the whole reader-facing corpus (census below): `started_at`, the
`holds[]` element schema (hold_id / fault_id / status / ttl_remaining_sec / stuck_at), and the fact that `holds` is the
COMPLETE listing (terminal rows included — daemon.go:316-322), which matters because `mischief status`'s default is
active-only: a reader who assumes health.json mirrors `status` mis-reads `holds` as live holds.

Absence census — corpus = README.md, docs/*.md, docs/reference/*.md, docs/prd/*.md, skills/mischief-usage/SKILL.md,
CONTRIBUTING.md, SECURITY.md. Control arms (keys known to be documented) prove the checker is not blind:

    ok               corpus=15    OPERATIONS.md=5
    daemon_boot      corpus=3     OPERATIONS.md=2      <- control
    pid              corpus=25    OPERATIONS.md=2
    run_dir          corpus=6     OPERATIONS.md=1
    holds            corpus=28    OPERATIONS.md=4
    stuck            corpus=15    OPERATIONS.md=1
    reverted         corpus=43    OPERATIONS.md=4
    revert_failed    corpus=28    OPERATIONS.md=5
    not_landed       corpus=7     OPERATIONS.md=2
    refold_error     corpus=5     OPERATIONS.md=2      <- control
    started_at       corpus=0     OPERATIONS.md=0      <- GAP
    generated_at     corpus=1     OPERATIONS.md=0      <- named only in the agent-side skill

    hold_id            corpus=0    <- GAP (word-boundary match; README's `hold_ids` is the status-JSON field, not this one)
    fault_id           corpus=1
    target             corpus=141
    status             corpus=65
    ttl_remaining_sec  corpus=0    <- GAP
    stuck_at           corpus=1    <- PRD §6.1, about `status`, not about health.json

The single `generated_at` hit is skills/mischief-usage/SKILL.md:370, which tells an agent to distrust `ok:true` when it
"cannot explain the health file's generated_at" — a field the operator reference never defines.

Also verified against the same live document: docs/OPERATIONS.md:64 "`ok:false` means either a `revert_failed` hold ... or a
journal corruption (`refold_error`)" — matches internal/daemon/daemon.go:349-357 exactly (stuck holds do NOT flip ok).
ACCURATE.

## Probe 6 — documented restore-drill and retention commands (T3)

    <bin> status --dir <scratch>/runs        -> run_dir/boot_id/holds: 0/proofs: 0, rc=0   (OPERATIONS.md:157)
    <bin> retention --runs-dir <scratch>/runs -> "DRY-RUN PLAN (nothing executed) retention: 0 action(s)", rc=0  (OPERATIONS.md:150)

Both documented forms execute on the audited tip. (Their pre-fix failures are tracked by open rows DOC-27 / DOC-29, both
in_progress; the in-tree text now names `--runs-dir` / `--dir`, so this pass records them as REPAIRED IN SOURCE, rows left
for the foreman to close.)

---

## Per-finding tier table

| finding | tier | what was actually run |
|---|---|---|
| DOC-38 health.json field reference incomplete | T4 | real `serve` process, health.json read back from its run dir for both an armed and a terminal hold |
| metrics table accuracy (no row) | T4 | live scrape of a real daemon, set-diff vs docs table |
| cadence 5s (no row) | T4 | cycles_total sampled across an 11s window on a running daemon |
| bind-failure non-fatal (no row) | T4 | two real daemons contending one port; stderr + continued health writes |
| DOC-31 premise re-verified (existing row) | T4 | full arm -> land -> fold loop through the daemon |
| DF-75 premise satisfied (existing row) | T4 | fresh-daemon first scrape carries not_landed 0 |

## Left UNVERIFIED

- The systemd unit recipe (OPERATIONS.md:25-42) was NOT installed or booted (no root unit path exercised); `MemoryMax=256M`
  and `Documentation=file:///usr/local/share/doc/...` are unprobed.
- `stuck_at` was never captured live — it requires a previous-boot leftover hold; its presence in the element is from source
  (daemon.go:93-94), not from a live byte capture.
- The `revert_failed` → `ok:false` path was not driven live (it needs a deliberately failing inverse); the claim was checked
  against internal/daemon/daemon.go:349-357 only.
- A prometheus-server-side scrape/alerting integration was not run; only the raw text exposition was read.
- Prior-delivery rows DOC-30 .. DOC-37 were NOT re-measured this tick.
