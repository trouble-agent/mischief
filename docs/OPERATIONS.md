# Operations — mischief / mischiefd

The operational surface of this repository: what runs, how it is supervised, the health
document, the Prometheus metrics endpoint (MISCHIEF-OPS-001) and how the fleet monitor should
scrape and alert on it.

## What runs

| unit | binary | surface |
|---|---|---|
| `mischief` | one static Go binary (`make bin`, `.goreleaser.yml` for releases) | CLI: `plan`, `arm`, `land`, `status`, `revert`, `doctor`, `selftest`, `install`, `uninstall`, `audit`, `retention`, `battery`, `chaos`, `sim`, `plane` |
| `mischiefd` | `mischief serve` | reverter daemon: owns TTLs, reconciles previous-boot leftovers, writes `health.json`, and (since MISCHIEF-OPS-001) serves Prometheus metrics |

The daemon owns ONE run dir (`--dir`, default `$MISCHIEF_RUN_DIR` / `~/.mischief/run`-style
resolution from `defaultRunDir()`); the journal in that dir is the durable state. A supervisor
may run one daemon per run dir.

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

## Health document (unchanged)

`mischief serve` writes `<run dir>/health.json` ATOMICALLY (temp + rename) on every state change
and on a 5 s heartbeat. It is a FILE, not a socket: a reader (`jq .ok health.json`) never needs to
talk to the daemon. `ok:false` means either a `revert_failed` hold (a fault may still be live) or a
journal corruption (`refold_error`). Key fields: `ok`, `daemon_boot`, `pid`, `run_dir`, `holds`,
`stuck`, `reverted`, `revert_failed`, `not_landed`.

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
| `mischiefd_revert_total{result}` | counter | revert attempts by the journal's closed outcome vocabulary: `reverted`, `revert_failed`, `no_hold` (the hold vanished between snapshot and revert — reported, not fatal) |
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

The daemon's durable state is the run-dir journal (`.jsonl` records) plus the terminal proofs it
writes. It is append-only and small; there is no database to dump. Two operator paths:

- `mischief retention --dir <run dir>` — report old run dirs; `--apply` rotates them. Report-only
  by default, so a cron can run it without deleting evidence.
- Snapshot the run dir with the filesystem's own snapshot/backup while the daemon runs (the
  journal is append-only, so a file copy is consistent enough for a restore into a scratch dir;
  there is no `VACUUM`-style live-mutation hazard because nothing rewrites the journal in place).

Restore drill: copy a run dir to a scratch path and read it with a second, throwaway daemon —
`mischief status --journal-dir <scratch>` prints the folded holds without touching the live run
dir, which is the cheap restore verification.

## Troubleshooting

| symptom | first probe |
|---|---|
| `ok:false` in health.json | `jq .refold_error,.revert_failed health.json`; a `refold_error` is corruption, a `revert_failed` is a live fault |
| metrics port silent, daemon alive | check the unit's `MISCHIEFD_METRICS_ADDR`; a bind failure is logged to stderr and is non-fatal by design |
| `mischiefd_cycles_total` flat | the sweep is not running: read the daemon's last stderr lines (a fatal revert error exits the loop) |
| stale health.json | compare `daemon_boot`/`pid` against the running process; a stale file after a restart means the supervisor did not start the daemon |
