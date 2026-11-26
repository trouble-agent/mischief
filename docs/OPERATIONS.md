# Operations — mischief / mischiefd

The operational surface of this repository: what runs, how it is supervised, the health
document, the Prometheus metrics endpoint (MISCHIEF-OPS-001) and how the fleet monitor should
scrape and alert on it.

## What runs

| unit | binary | surface |
|---|---|---|
| `mischief` | one static Go binary (`make bin`, `.goreleaser.yml` for releases) | CLI: `plan`, `arm`, `land`, `status`, `revert`, `doctor`, `selftest`, `install`, `uninstall`, `audit`, `retention`, `battery`, `chaos`, `sim`, `plane` |
| `mischiefd` | `mischief serve` | reverter daemon: owns TTLs, reconciles previous-boot leftovers, writes `health.json`, and (since MISCHIEF-OPS-001) serves Prometheus metrics |

The daemon owns ONE run dir; the journal in that dir is the durable state. The default is
`$HOME/.mischief/runs/default` (resolved by `defaultRunDir()`, `cmd/mischief/main.go:221-229`).
No environment variable selects the run dir: `--dir` on each journal-touching verb is the only
override. A supervisor may run one daemon per run dir.

The run dir is single-writer by construction (MISCHIEF-DF-71): `serve` holds an exclusive
`flock` on `<run dir>/mischiefd.lock` for its whole lifetime, so a second `serve --dir <the same
dir>` is REFUSED — nonzero exit, the owning pid named on stderr, no store opened, no file
touched:

```
$ mischief serve --dir $D; echo rc=$?
serve: daemon: run dir is already owned by a live daemon: $D is held by live pid 4242 (one daemon per run dir — stop it or pass a different --dir)
rc=1
```

The lock dies with its holder — a killed daemon leaves a stale lock FILE that the next `serve`
simply adopts, so there is no stale-lock cleanup step — and `health.json` therefore has exactly
one writer, which is why its `pid` field can only ever name the daemon that wrote that document.
The revert path enforces the same single-owner discipline ACROSS processes: each attempt holds
`<run dir>/revert.lock` while it re-reads the journal, executes the recorded inverse and appends
the proof, so two daemons (or a daemon and a concurrent `revert --hold`) never execute one hold's
inverse twice — the loser declines with `reverter: another process already executed a revert
attempt for this hold` and writes nothing.

## Supervision

There is no Dockerfile and no in-repo systemd unit: `mischief` is a single static binary and its
deploy shape is a unit file per run dir. The unit below is the canonical recipe — install the
binary (see README "Install"), copy the unit, `systemctl daemon-reload && systemctl enable
--now mischiefd@<rundir>`.

```ini
[Unit]
Description=mischief reverter daemon (run dir %i)
After=network-online.target
Documentation=file:///usr/local/share/doc/mischief/OPERATIONS.md

[Service]
Type=simple
ExecStart=/usr/local/bin/mischief serve --dir %i
Environment=MISCHIEFD_METRICS_ADDR=127.0.0.1:9464
Restart=always
RestartSec=2
# the daemon must not be OOM-killed while holding TTLs
MemoryMax=256M

[Install]
WantedBy=multi-user.target
```

`Restart=always` is the correct policy: a daemon that dies leaves its holds unowned, so the
supervisor must bring it back and the metric/exposition gap (below) is what makes that visible
instead of silent.

## TTL sweep timing

`mischief serve` runs the hold sweep every `HeartbeatInterval`, which is 5s in
`internal/daemon/daemon.go`. A hold is reverted at the next sweep after its
expiry, so it is never reverted before its declared TTL and the maximum
overshoot after expiry is at most 5s. Operationally, a 6s TTL can therefore
contain a fault for up to about 11s (`6s + 5s`) before the next sweep. The
executable regression `internal/daemon/daemon_test.go::TestDaemonTTLNeverEarlyAndRevertsWithinHeartbeatInterval`
asserts both bounds through the real journal and daemon loop.

## Health document (unchanged)

`mischief serve` writes `<run dir>/health.json` ATOMICALLY (temp + rename) on every state change
and on a 5 s heartbeat. It is a FILE, not a socket: a reader (`jq .ok health.json`) never needs to
talk to the daemon. `ok:false` means either a `revert_failed` hold (a fault may still be live) or a
journal corruption (`refold_error`).

Every key the document carries (`internal/daemon/daemon.go:46-95` is the struct; the list below was
read off a live capture, not off the struct alone):

| key | meaning |
|---|---|
| `ok` | `true` while the daemon folds state and no hold sits in the failed terminal state; stuck leftovers from a previous boot do NOT flip it (they are a reported condition, not a daemon defect) |
| `daemon_boot` | THIS daemon process's boot id (hex16), minted fresh at each start — key on it to tell a stale file from the live daemon's; it is NOT the reverter fold's host-boot-scoped id |
| `pid` | the daemon process's pid |
| `run_dir` | the absolute run dir (the journal's home) |
| `started_at` | the daemon's start time, unix seconds |
| `generated_at` | this document's write time, unix seconds — the STALENESS datum: the heartbeat is 5 s, so compare it against `date +%s`; a delta of more than a couple of beats means the daemon stopped beating and `ok:true` then says nothing about the run |
| `holds` | the COMPLETE listing: one row per known hold, terminal rows included (see the surface warning below) |
| `stuck` | count of holds that boot reconcile marked stuck (previous boots' leftovers) |
| `reverted` | count of holds with a measured revert proof |
| `revert_failed` | count of holds whose measured revert FAILED — the fault may still be live, and this is the one hold state that flips `ok` false |
| `not_landed` | count of holds whose proof recorded that the fault was NEVER landed: not a pass and not a failure (no fault was applied, so the run carries no integrity signal) |
| `refold_error` | the journal re-fold error string, present ONLY when non-empty (`omitempty`): the document then carries the LAST KNOWN fold with `ok:false`, never a silent stale view |

`holds` is the COMPLETE listing, and that is NOT the surface `mischief status` shows by default:
`status` lists ACTIVE holds only (`armed` / `stuck`) and keeps terminal rows off that listing —
passing `--all` to that same command is the surface that matches this document. So a `reverted` or `not_landed`
row here is history, not a live fault: do not read a populated `holds` as "faults are live", and do
not read `status`'s `holds: 0` as "the journal knows no holds". The `reverted`, `revert_failed` and
`not_landed` counters are derived from exactly the terminal rows the active-only default omits.

`holds[]` element keys:

| key | meaning |
|---|---|
| `hold_id` | the hold's stable id |
| `fault_id` | the catalog fault the hold was armed for (e.g. `S-001`) |
| `target` | the target selector the hold was armed on (e.g. `scratch:/path/to/target`) |
| `status` | the hold's state: `armed`, `stuck`, `reverted`, `revert_failed`, or `not_landed` |
| `ttl_remaining_sec` | float seconds left before the sweep reverts the hold — ARMED ROWS ONLY (`omitempty`) |
| `stuck_at` | unix seconds at which boot reconcile marked the hold stuck — STUCK ROWS ONLY (`omitempty`) |

A healthy document therefore carries 11 top-level keys (`refold_error` is omitted while empty), and
each hold row carries the keys its own state uses — never all six.

Captured live at the audited tip: `serve` over a scratch run dir, one `S-001` hold armed with a
60 s TTL, read 7 s in. (The captures below abbreviate the scratch run dir as `/tmp/doc38-cap`; every
id, timestamp and float is the literal value the capture printed.)

```
$ jq -r '. | keys[]' $D/health.json
daemon_boot
generated_at
holds
not_landed
ok
pid
revert_failed
reverted
run_dir
started_at
stuck
$ jq -r '.holds[0] | keys[]' $D/health.json
fault_id
hold_id
status
target
ttl_remaining_sec
$ jq -c . $D/health.json
{"ok":true,"daemon_boot":"48b24283adb966d6","pid":1364621,"run_dir":"/tmp/doc38-cap/runs","started_at":1795157465,"generated_at":1795157495,"holds":[{"hold_id":"3cf330efeecef9ca","fault_id":"S-001","target":"scratch:/tmp/doc38-cap/target","status":"armed","ttl_remaining_sec":48.415974816}],"stuck":0,"reverted":0,"revert_failed":0,"not_landed":0}
```

The two conditional element keys appear only on the rows that use them. `ttl_remaining_sec` is in
the armed row above; a previous-boot leftover (same daemon, the run dir's `boot.json` `host_boot`
rewritten to simulate a reboot, which is the reconcile path a real reboot takes) renders:

```
$ jq -r '.holds[0] | keys[]' $D/health.json
fault_id
hold_id
status
stuck_at
target
$ jq -c '.holds[0], .stuck' $D/health.json
{"hold_id":"3cf330efeecef9ca","fault_id":"S-001","target":"scratch:/tmp/doc38-cap/target","status":"stuck","stuck_at":1795157698}
1
```

And the complete-vs-active split, measured on one run dir holding a single `reverted` (terminal)
hold while the daemon serves it:

```
$ jq -c '.holds' $D/health.json     # health: the COMPLETE listing
[{"hold_id":"0db0b502ec63ebca","fault_id":"S-001","target":"scratch:/tmp/doc38-cap/term/t","status":"reverted"}]
```

Read the same run dir through the CLI and the counts disagree on purpose: `status` reports
`holds: 0` (active rows only) while `status --all` reports `holds: 1`, matching this document.
Use `--all` whenever you are reconciling against `health.json`.

## Metrics endpoint (MISCHIEF-OPS-001)

The daemon deliberately serves no HTTP on its health surface, so the metrics surface is a
SEPARATELY BOUND listener, loopback by default:

| env var | default | meaning |
|---|---|---|
| `MISCHIEFD_METRICS_ADDR` | `127.0.0.1:9464` | listen address for `GET /metrics`; `off` (or `disabled`) turns the surface off |

```bash
MISCHIEFD_METRICS_ADDR=127.0.0.1:9464 mischief serve --dir /var/lib/mischief/run
curl -s http://127.0.0.1:9464/metrics | grep '^mischiefd_'
```

A bind failure is logged (`serve: metrics: daemon: metrics listen …`) and does NOT stop
containment: losing the metric surface must never stop the daemon from reverting faults.

### Families

| metric | type | meaning |
|---|---|---|
| `mischiefd_up` | gauge | 1 while the exposition answers |
| `mischiefd_start_time_seconds` | gauge | process start (unix seconds) |
| `mischiefd_cycles_total` | counter | completed heartbeat/sweep cycles |
| `mischiefd_scrapes_total` | counter | scrapes served |
| `mischiefd_revert_total{result}` | counter | revert attempts by the journal's closed outcome vocabulary: `no_hold`, `not_landed`, `revert_failed`, `reverted` (`no_hold` means the hold vanished between snapshot and revert — reported, not fatal) |
| `mischiefd_errors_total{reason}` | counter | daemon error paths: `refold` (Journal corruption held for the render — DEGRADED, the loop survives), `revert` (a revert attempt failed fatally), `health_write` (health.json publish failed) |
| `mischiefd_revert_duration_seconds` | histogram | one revert attempt end to end (action + measured check), buckets 5 ms … 10 s |

Labels are bounded by construction: `result` is the journal's `Outcome` vocabulary plus
`no_hold`, `reason` is a fixed three-token classification. No free-form message ever becomes a
label, so a scrape cannot mint unbounded series.

### Scrape config (fleet monitor)

```yaml
scrape_configs:
  - job_name: mischiefd
    static_configs:
      - targets: ["127.0.0.1:9464"]
    metrics_path: /metrics
    scrape_interval: 15s
```

If the daemon runs on another host, expose the port over an authenticated proxy — never bind a
public interface: the endpoint has no auth of its own.

### Alerts

```yaml
- alert: MischiefdDown
  expr: up{job="mischiefd"} == 0
  for: 2m
  annotations:
    summary: mischiefd metrics unreachable - TTL ownership unverified

- alert: MischiefdCyclesStalled
  expr: rate(mischiefd_cycles_total[15m]) == 0
  for: 10m
  annotations:
    summary: mischiefd is up but not sweeping (frozen health, holds unowned)

- alert: MischiefdRevertFailing
  expr: increase(mischiefd_revert_total{result="revert_failed"}[1h]) > 0
  annotations:
    summary: a revert was MEASURED as failed - the fault may still be live

- alert: MischiefdCorruptJournal
  expr: increase(mischiefd_errors_total{reason="refold"}[15m]) > 0
  annotations:
    summary: journal corruption held by the daemon - health.json renders ok:false
```

`MischiefdCyclesStalled` is the alert the health FILE cannot give: a wedged daemon keeps its last
`health.json` on disk, so "the file is stale" is invisible to a file reader but immediate from a
flat `mischiefd_cycles_total`.

## Backup and retention

The daemon's durable state is the run dir's JSONL journals — the verbs' run
journals (`plan.jsonl`, `selftest.jsonl`, `battery.jsonl`, the internal/journal
envelope documented under "Run-dir journal record schema" below) plus the
reverter's own journal `reverter.jsonl` (`internal/reverter`'s envelope, its own
section below) — and the terminal proofs it writes. All of them are append-only
and small; there is no database to dump. Two operator paths:

- `mischief retention --runs-dir <run dir>` — report old run dirs; `--apply` rotates them. Report-only
  by default, so a cron can run it without deleting evidence.
- Snapshot the run dir with the filesystem's own snapshot/backup while the daemon runs (the
  journal is append-only, so a file copy is consistent enough for a restore into a scratch dir;
  there is no `VACUUM`-style live-mutation hazard because nothing rewrites the journal in place).

Restore drill: copy a run dir to a scratch path and read it with the CLI —
`mischief status --dir <scratch>` prints the folded holds and proofs without touching the live run
dir, which is the cheap restore verification.

## Run-dir journal record schema

The run-dir journals are JSONL — one record per line, appended by the verb that owns that phase
of the run. `plan.jsonl` (written by `plan --journal-dir D`), `selftest.jsonl` and
`battery.jsonl` share the same record envelope (`internal/journal/record.go`), so a reader that
folds one folds all of them.

The envelope — each line is one `Record` (`internal/journal/record.go:52-67`):

| field | type | presence | meaning |
|---|---|---|---|
| `type` | string, closed set of ten | always | the record type (vocabulary below). The write path refuses an unknown type and the read path fails loudly on one rather than folding it into a summary. |
| `run_id` | string | always | the content-derived run id, 16 hex chars: `hex(sha256("seed\n<seed>\n" + spec_bytes))[:8]` (`RunID`, `internal/journal/experiment.go:419`). Scrubbed on write like any free-form field. |
| `seq` | int | always | the run-local sequence number: 0-based, monotonic in append order |
| `ts` | int64 | always | unix timestamp, seconds |
| `verdict` | string, closed vocabulary | only on `verdict` and `run_closed` records | the graded verdict value: `recovered`, `degraded`, `not_recovered`, `hung`, `corrupted`, `no_op`, `aborted`, `flaky` (PRD §6.4). Write validation refuses a verdict on any other record type. |
| `fields` | object (string → string) | optional, omitted when empty | record-specific detail. Every value is a JSON string — never a number; numeric-looking values ride as strings (see the example below). Values are scrubbed on write, and the fully serialised line gets a byte-level scrub backstop. |

### The ten record types

Each `type` and when the binary writes it (`internal/journal/record.go:16-34`; write sites as
named — "no writer yet" is grep-provable in the current tree):

| `type` | written when |
|---|---|
| `run_planned` | the resolved plan before any side effect (AC-1): once per `plan --journal-dir D`, and as the opening record of the `selftest` and `battery` journals |
| `fault_armed` | the fault actuator is armed, pre-land (`selftest`) |
| `fault_landed` | carrying the landing proof (`selftest`, `battery`) |
| `probe_sample` | one probe observation — declared in the vocabulary; no writer yet in the current tree |
| `assertion_result` | one probe/assertion grading — declared in the vocabulary; no writer yet in the current tree |
| `observe_result` | the observe.trouble outcome (AC-14) — the record builder lives in `internal/observe`; wiring a verb to it is tracked as future (`internal/battery/matrix.go`) |
| `fault_reverted` | carrying the revert proof (AC-4) (`selftest`, `battery`) |
| `verdict` | the graded verdict value (AC-9): appended by the `selftest` and `battery` graders and rendered by the verdict engine (`Result.Record`, `internal/verdict/engine.go:379`) |
| `run_closed` | the run's terminal record, carrying the same verdict (`selftest`, `battery`) |
| `finding` | a finding filed from the run (AC-18 shape) — declared in the vocabulary; no writer yet in the current tree |

### Example (captured, not hand-written)

`MISCHIEF_SANCTION=1 MISCHIEF_SANCTION_FILE=<marker file> mischief plan -f
catalog/experiments/ex-001-noop-detector.yaml --journal-dir <scratch>/j` appends one line to
`<scratch>/j/plan.jsonl`, verbatim:

```json
{"type":"run_planned","run_id":"84c8a84cecf315c1","seq":0,"ts":1795040158,"fields":{"catalog_version":"6ba6eda1a66b080788f89193011d5ee45e33281895553748b620a7dddf999556","experiment":"ex-001-noop-detector","faults":"F-008","faults_count":"1","kind":"plan","mode":"preview","refusals":"0","seed":"20260927","target_kind":"host","target_selector":"host:."}}
```

`"faults_count":"1"` and `"refusals":"0"` are the envelope rule in action: counters ride
`fields` as JSON strings, never numbers.

## The reverter journal: reverter.jsonl (internal/reverter)

The verbs' run journals above are one family; the reverter keeps its OWN journal
in the same run dir: `<run dir>/reverter.jsonl` — `~/.mischief/runs/default/reverter.jsonl`
by default, or the `--dir` the verb was given (`internal/reverter/journal.go`,
`journalFileName`; file mode 0600, created on first `Open`). It is a separate
append-only JSONL with a separate envelope and a six-record vocabulary
(`internal/reverter/record.go:16-38`) — do not fold it with the
internal/journal readers above.

**Writer/reader relationship.** The writer is the reverter `Store`'s append
path in `internal/reverter/journal.go`: every write is serialised through the
store's mutex because the CLI, the TTL owner and a concurrent kill switch all
write the same file, and lines are never rewritten in place. The reader is the
same package's fold: `Open` replays the whole journal once (this is also where
boot reconcile marks previous-boot holds `stuck`) and refreshes state
incrementally on each append, so `status`, `revert`, `serve` and the daemon's
per-tick health fold all read the same log. Journal corruption surfaces as the
daemon's `refold_error` (held by the daemon, rendered `ok:false` in
`health.json`) — see the troubleshooting table below and the torn-journal
ladder in `skills/mischief-usage/SKILL.md`.

**Envelope keys** (one JSON object per line, `internal/reverter/record.go:441-461`):

| key | type | presence | meaning |
|---|---|---|---|
| `type` | string, closed set of six | always | the record type (vocabulary below); the write path refuses an unknown type |
| `hold_id` | string | always | the content-derived hold id every record belongs to (the same declaration armed twice yields the same id) |
| `ts` | int64 | always | unix timestamp, seconds |
| `boot_id` | string | always | the REVERTER boot id from the run dir's `boot.json` (`reverter_boot` field) — NOT the host's `/proc/sys/kernel/random/boot_id`; boot reconcile compares this against arm records to find previous-boot leftovers |
| `own_pid` | int | optional | the pid of the process that EXECUTED the thing the record describes (the reverter pid for owner-executed reverts, the landing pid for `land`) |
| `outcome` | string | only on `revert_proof` | the measured revert outcome: `reverted` \| `revert_failed` (closed vocabulary, enforced at the write path) |
| `payload` | object | only on `arm` | the full write-ahead hold declaration (fault, target, inverse, check, TTL), carried verbatim so a detached owner process can execute it from journal bytes alone |
| `fields` | object (string → string) | optional, omitted when empty | record-specific detail; values are scrubbed on write |

**The six record types** (`internal/reverter/record.go:16-38`):

| `type` | written when |
|---|---|
| `arm` | the WRITE-AHEAD record: the hold (fault, inverse, check, TTL) is on disk and fsynced before `land` may be called — a hold whose arm line is not durable must never be landed |
| `land` | the caller landed the fault (post write-ahead), naming the landing pid in `own_pid` |
| `land_refused` | a Land refusal: the write-ahead line for the hold was not durable, so the inverse would not predate the fault — nothing was landed |
| `hold_expired` | the TTL boundary observed by a TTL owner (`own_pid` set; boot reconcile records it with `own_pid` absent) |
| `revert_proof` | the MEASURED revert outcome (AC-4): fault id, measured check, `outcome` (`reverted` \| `revert_failed`), who, pid |
| `hold_stuck` | a non-terminal hold inherited from a previous boot, marked by boot reconcile at `Open` (AC-6's "leftover faults from a previous boot are listed stuck") |

**Backup/restore collection path.** The generic run-dir snapshot described
under "Backup and retention" above covers this journal: a filesystem snapshot
of the run dir collects `reverter.jsonl` together with the verbs' run journals,
and the restore drill is the same (`mischief status --dir <scratch copy>` folds
the reverter journal without touching the live one). Because the file is
append-only and never rewritten in place, a file copy taken while the daemon
runs is consistent enough for a restore into a scratch dir.

## Troubleshooting

| symptom | first probe |
|---|---|
| `ok:false` in health.json | `jq .refold_error,.revert_failed health.json`; a `refold_error` is corruption, a `revert_failed` is a live fault |
| metrics port silent, daemon alive | check the unit's `MISCHIEFD_METRICS_ADDR`; a bind failure is logged to stderr and is non-fatal by design |
| `mischiefd_cycles_total` flat | the sweep is not running: read the daemon's last stderr lines (a fatal revert error exits the loop) |
| stale health.json | compare `daemon_boot`/`pid` against the running process; a stale file after a restart means the supervisor did not start the daemon |
