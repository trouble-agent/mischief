# mischief — fault catalog (v0.1 draft)

The catalog is **data**. Every entry carries, by contract: what it does, the
**independent landed-proof** (how the run knows the fault arrived), the **inverse**
(how it is reverted and how the revert is measured), the **capability** it needs, and
the **privilege tier** (L0 scratch / L1 rootless scope / L2 sudo allowlist / L3 docker /
L4 simulator). A primitive that cannot state all five does not enter the catalog.

**Legend** — backend: `sig` signals, `shim` LD_PRELOAD, `sec` seccomp user-notify,
`scope` `systemd-run --user` scope, `helper` narrow sudo allowlist, `net` netns+netem,
`dm` device-mapper/loop, `docker` container API, `proxy` userspace HTTP/DNS/TLS proxy,
`sim` provider simulator. Tier shorthand in the Cap column.

**Why landed-proofs are not optional**: a chaos run that cannot distinguish *"the fault
landed and the software survived"* from *"the fault never landed"* reports the second
as the first. That is exactly how this fleet's `chaos-corruption` cell passed for weeks
(QA-TROUBLE-3).

---

## P — Process & signal (14)

| ID | fault / what it breaks | landed-proof | inverse | Cap |
|---|---|---|---|---|
| P-001 | `SIGSTOP` a process (hang, no crash): drains, heartbeats, watchdog logic | `/proc/<pid>/stat` state `T` (or `/proc/<pid>/status` `SigStopped`) for N consecutive samples | `SIGCONT` + assert state `R/S` | sig · L1 |
| P-002 | `SIGSTOP` **one thread** of a multi-threaded daemon (`tgkill`) — thread starvation, blocked work queue | thread's per-tid state `T` while the process stays alive | `SIGCONT` to that tid | sig · L1 |
| P-003 | `SIGKILL` (crash, no cleanup) mid-operation | target pid gone before its exit record; `waitpid` says signal 9 | restart (process) / none needed (container) | sig · L1 |
| P-004 | `SIGTERM` **ignored** on purpose (stuck drain) — shutdown never completes | process alive past `SIGTERM` + grace window | escalation `SIGKILL`, then restart | sig · L1 |
| P-005 | `SIGTERM` followed by graceful-grace expiry → escalation: shutdown path never finishes cleanly | the KILL was necessary (the target did not exit within grace) | restart | sig · L1 |
| P-006 | **Kill between two named steps** (e.g. enqueue → drain, append → fsync, ack → commit) — the "durable in name only" class (TRBL-041) | the intermediate state EXISTS on disk (spool entry present, record unwritten) and the process is gone | restart; assert the intermediate state is replayed or honestly discarded | sig+shim · L1 |
| P-007 | Kill the **parent** (orphan / re-parent to init / lost subreaper) | child's `PPid` changed to 1 (or the target's own subreaper) | `SIGKILL` the orphan; report the leak | sig · L1 |
| P-008 | Kill **PID 1 of a container** (whole-container death) | `docker inspect` `State.Running=false`, exit code 137/143 | `docker start` (or restart policy) | docker · L3 |
| P-009 | `SIGSEGV`/`SIGABRT`/`SIGBUS` injection (crash with core) — panic paths, corrupted-state recovery | process died BY that signal (`WTERMSIG`) | restart; assert no half-written state | sig · L1 |
| P-010 | `SIGPIPE` on a listener (peer vanished) — write to a dead consumer | `EPIPE` observed on the writer side | reconnect / restart the consumer | sig · L1 |
| P-011 | Real-time signal storm (`SIGRTMIN+k`) interrupting blocking syscalls — `EINTR` loops, `sigtimedwait` bugs | `EINTR` observed in the target's own log/counters; signal delivery count via `/proc/<pid>/status` | stop the storm; assert the call completes | sig · L1 |
| P-012 | `SIGSTOP` while **inside a write/syscall** and never resume until after the observer samples (torn/flush-window faults) | state `T` + the target's fd open, target file size unchanged | `SIGCONT`, then compare integrity oracle | sig+shim · L1 |
| P-013 | Cgroup **freeze** (`cgroup.freeze=1`) — the whole tree stalls at once, unlike per-process stop | `cgroup.events` frozen=1 **or** every pid in the group state `T` | `cgroup.freeze=0` + assert every pid resumed | helper · L2 |
| P-014 | **Crash loop**: restart N times with a backoff gate, then stop (container `restart=always` with a failing entrypoint) | `docker inspect` `RestartCount` ≥ N | restore entrypoint/start command; assert stable uptime | docker · L3 |

**Edge cases this layer exists for**: graceful shutdown that only works when nothing is
in flight; a drain that hangs forever with zero error output; a watchdog that stops
watching when the watchdog itself is stopped; recovery that depends on the process
having exited *cleanly*.

---

## S — Syscall & libc (14)

| ID | fault / what it breaks | landed-proof | inverse | Cap |
|---|---|---|---|---|
| S-001 | `write()`/`pwrite()` to a **named path** returns `EIO` | shim's own per-pid counter file increments (independent of the target) | counter stops; next write succeeds | shim · L1 |
| S-002 | `write()` returns `ENOSPC` (disk full without a full disk) | counter + the target's own error line | counter stops | shim · L1 |
| S-003 | `write()` returns `EDQUOT` (quota, distinct from ENOSPC) | counter + target's error | counter stops | shim · L1 |
| S-004 | **Short write**: `write()` returns `n/2` (partial write, no error) — the nastiest shape, no error anywhere | counter + the file's size/content mismatch vs expectation | disable rule; assert the app's own append logic | shim · L1 |
| S-005 | `fsync`/`fdatasync`/`sync_file_range` fails `EIO` — durability acknowledged but not durable | counter + the target's log | counter stops; re-fsync | shim/seccomp · L1 |
| S-006 | `rename`/`link` fails `EXDEV`/`EACCES` — atomic publish path breaks | counter | retry path succeeds | shim · L1 |
| S-007 | `open`/`openat` of a path returns `EACCES`/`ENOENT` (the file exists) | counter + target's error | counter stops | shim · L1 |
| S-008 | `connect`/`send`/`recv` fail `ECONNREFUSED`/`ETIMEDOUT` for a named peer | counter | counter stops | shim/seccomp · L1 |
| S-009 | `malloc`/`calloc` returns `NULL` at the Nth allocation — allocation-failure paths that never run | counter (allocation index) | counter stops | shim · L1 |
| S-010 | `EINTR` injected into a chosen blocking syscall (select/poll/read) — retry-loop correctness | counter | counter stops | shim · L1 |
| S-011 | **Delay** injected before a chosen syscall (e.g. 500 ms before each `fsync`) — timeout budgets | counter + observed latency delta | rule removed; latency recovers | shim/seccomp · L1 |
| S-012 | **TOCTOU window**: widen the gap between a check and a use (delay after `stat`, before `open`) | the injected delay is visible in the syscall trace | rule removed | seccomp · L1 |
| S-013 | Seccomp-level **syscall-subset kill** (`SECCOMP_RET_ERRNO` for a family) — graceful degradation when a syscall disappears | supervisor's notification count per syscall | supervisor detached, filter dropped with the process | sec · L1 |
| S-014 | `getrandom`/`/dev/urandom` returns a **fixed seed** — reproducibility bugs, key reuse (test-only) | counter; identical outputs across runs | rule removed | shim · L1 |

**Edge cases**: error paths that have never executed; code that treats `EINTR` as fatal;
buffered writers that swallow a partial write; `fsync` failures that are logged and then
ignored; allocation-failure handling that assumes `NULL` never happens.

---

## R — Resource & cgroup (12)

| ID | fault / what it breaks | landed-proof | inverse | Cap |
|---|---|---|---|---|
| R-001 | `MemoryMax` squeeze (`memory.max`) — allocation failure + OOM kill under a hard cap | `memory.events` `oom`/`oom_kill` counters move, or target observes `ENOMEM` | raise the cap; assert the target's heap returns to baseline | scope · L1 |
| R-002 | `MemoryHigh` soft pressure (throttling, `memory.pressure` climbs) | `memory.pressure` `some`/`full` rises above baseline | clear the limit | scope · L1 |
| R-003 | **OOM-kill a chosen member** (`memory.oom.group` + `oom_score_adj`) — which part of a tree dies first | kernel log/`dmesg` OOM line naming the pid + the pid is gone | restart the victim in the correct order | scope/helper · L1 |
| R-004 | `CPUQuota` throttle (starved CPU, timing budget blown) | `cpu.stat` `nr_throttled` + `throttled_usec` grow | remove the quota; latency recovers | scope · L1 |
| R-005 | CPU **pin to one core / cpuset shrink** — concurrency bugs stop hiding | `cpuset.cpus.effective` narrows; observed parallelism drops | restore the cpuset | scope · L1 |
| R-006 | `TasksMax`/`RLIMIT_NPROC` squeeze — `fork`/`clone` returns `EAGAIN` | the target observes `EAGAIN`; `pids.current` at the cap | raise the limit | scope · L1 |
| R-007 | `RLIMIT_NOFILE` squeeze — fd exhaustion (`EMFILE`), deferred by design | the target observes `EMFILE`; `ls /proc/<pid>/fd` at the cap | raise the limit; assert fds are released | scope/shim · L1 |
| R-008 | `io.max`/`blkio` throttle — slow disk under a latency budget | `io.stat` throttled counters / observed latency delta | remove the throttle | scope · L1 |
| R-009 | **Swap/memory pressure** with no OOM (death by a thousand page faults) | PSI `full` grows, no OOM kill | clear pressure, measure recovery time | scope · L1 |
| R-010 | Hugepage exhaustion (`nr_hugepages` to 0) — silent failure of a hugepage-dependent allocator | hugepage free counters drop to zero | restore | helper · L2 |
| R-011 | **Zombie accumulation**: stop reaping (freeze the parent) — pid exhaustion via unreaped children | `ps` state `Z` count grows; `pids.current` climbs | `SIGCONT` parent; children reaped | sig · L1 |
| R-012 | File-descriptor **leak injection** (shim holds a reference per call) — leak detection over hours | shim's internal open-fd count grows monotonically | rule removed; fds released | shim · L1 |

---

## F — Filesystem & storage (16)

| ID | fault / what it breaks | landed-proof | inverse | Cap |
|---|---|---|---|---|
| F-001 | **Disk full** (`ENOSPC`) on a scratch mount, to the byte | `df` shows 0 available AND the target observes `ENOSPC` | delete the filler; `df` recovers | helper · L2 |
| F-002 | **Quota exhausted** (`EDQUOT`) with free space on the device | quota report + target's error | raise the quota | helper · L2 |
| F-003 | **Read-only remount** (`EROFS`) — write path dies live, reads keep working | `/proc/mounts` shows `ro`; writes raise `EROFS` | remount `rw`; assert an append succeeds | helper · L2 |
| F-004 | **`fsfreeze`** (FIFREEZE ioctl) — every writer blocks mid-flight for the freeze duration | ioctl returned 0 in the injector; target's writes are observed blocked | `FITHAW`; assert blocked writers complete | helper · L2 |
| F-005 | **dm-error** (real I/O errors on read/write for a mapped region) | `dmsetup status` error counter increments | remove the mapping; reads succeed | dm · L2 |
| F-006 | **dm-delay** (slow I/O, e.g. 200 ms per op) — timeout budgets, retry storms | measured latency delta + dm status | remove the delay | dm · L2 |
| F-007 | **Volume detach** (loop/mapper tear-down mid-write) — the cloud "volume gone" shape | the mount vanishes; `EIO`/`ENODEV` observed | re-attach; assert recovery (or honest refusal) | helper · L2 |
| F-008 | **Truncate to 0** under a live writer (log/DB/journal file) | file size 0 while the writer holds an fd | restore from the pre-fault copy | L0/L1 |
| F-009 | **File replaced under a live process** (`rename`-over an open fd) — the 2026-08-29/30 archive hole | inode of the open fd ≠ inode at the path (`stat` both) | restore the original path/inode; assert the writer recovers or dies honestly | L0/L1 |
| F-010 | **Directory removed** under a running service (config/cache dir gone) | `ENOENT` on the next relative operation | recreate; assert re-init vs crash | L0 |
| F-011 | **Bit-flip corruption** at a chosen offset (checksum mismatch detection) | the file's own hash differs by exactly the flipped bits | restore from baseline; assert the app detects it | L0 |
| F-012 | **Inode exhaustion** (millions of empty files) — `ENOSPC` with free space | `df -i` at 0 free inodes | delete the filler | helper · L2 |
| F-013 | **mtime/ctime frozen** (`os.Chtimes`) — change detectors that trust timestamps (the TRBL-013 class) | mtime unchanged while content differs | restore the timestamp | L0 |
| F-014 | **Permission flip** (`chmod 000`) on a live path — `EACCES` where none was expected | mode bits read back as 000 | restore the mode | L0 |
| F-015 | **Symlink/hardlink trickery**: path swapped to a new target mid-run (or a symlink loop) — path trust bugs | `readlink` shows the new target; `ELOOP` if looped | restore the link | L0 |
| F-016 | **Network/FUSE mount stall** (server unreachable → `D` state, uninterruptible) — the nastiest hang | process state `D` in `/proc/<pid>/stat`; `stat` on the mount blocks | restore the server; assert the client recovers (or is honest about `stuck`) | L1/L2 |

---

## N — Network, DNS, TLS (18)

| ID | fault / what it breaks | landed-proof | inverse | Cap |
|---|---|---|---|---|
| N-001 | **Full partition** (`netem loss 100%` or `DROP`) for a named peer/port | `tc -s qdisc` shows drops ≥ 1; the observer's connect fails | delete the qdisc; a request succeeds | net · L2 |
| N-002 | **Latency** injection (delay ± jitter) — timeout budgets, head-of-line blocking | `tc -s` delay counter + measured RTT delta | delete qdisc | net · L2 |
| N-003 | **Packet reorder / duplication** — protocol assumptions | netem stats + duplicate counts at the observer | delete qdisc | net · L2 |
| N-004 | **Bandwidth ceiling** (rate limit) — slow consumer/backpressure | measured throughput drop | delete qdisc | net · L2 |
| N-005 | **Packet corruption** (bad checksums, silent retransmits) | netem `corrupt` counter | delete qdisc | net · L2 |
| N-006 | **MTU blackhole** (drop packets > 1400) — the classic "hangs on large responses" | large packets dropped, small ones pass (`ping -s` split) | delete qdisc | net · L2 |
| N-007 | **Connection refused** (nothing listening) — cold start ordering | `ECONNREFUSED` observed by the target | start the listener | proxy · L0 |
| N-008 | **Half-open / silent drop after SYN** (connect hangs, no RST) — the worst timeout shape | observer sees SYN with no SYN-ACK; target blocked in connect | remove the rule | net/proxy · L1 |
| N-009 | **RST mid-response** (peer dies holding a half-written body) | the consumer observes `ECONNRESET` mid-body | re-request; assert no partial write leaked | proxy · L0 |
| N-010 | **Slow reader / backpressure** (consumer stops reading) — unbounded buffers | sender's socket buffer fills; write blocks | resume reading | proxy · L0 |
| N-011 | **Truncated HTTP body** (content-length lies) | client's parse error + received-vs-declared byte counts | remove the rule | proxy · L0 |
| N-012 | **HTTP 429 / 503 / 500 bursts** with/without `Retry-After` — backoff, retry storms | proxy's own hit counter + the target's retry log | rule removed | proxy · L0 |
| N-013 | **Chunked-encoding / gzip corruption** — parser robustness | client-side decode error | remove rule | proxy · L0 |
| N-014 | **Redirect loop** / wrong `Location` | client's hop counter | remove rule | proxy · L0 |
| N-015 | **DNS NXDOMAIN / SERVFAIL / timeout** for a named name | resolver-side counter; target's resolution error | restore the answer | proxy/helper · L1 |
| N-016 | **DNS stale/wrong record** (name resolves to a dead IP) — cache/TTL bugs | resolution returns the injected IP | restore; assert cache TTL respected | proxy/helper · L1 |
| N-017 | **TLS: expired / self-signed / wrong-host certificate**, or a **rotation mid-flight** | handshake failure in the client; the served cert's validity read back | restore the cert | proxy · L0 |
| N-018 | **Clock-skewed peer** (cert "not yet valid" / signature window) | handshake failure naming validity | restore | proxy · L0 |

---

## C — Container, node & orchestration (12)

| ID | fault / what it breaks | landed-proof | inverse | Cap |
|---|---|---|---|---|
| C-001 | Graceful stop with a short grace window (`docker stop -t 1`) — drain vs SIGKILL | `State.Running=false` + exit code; SIGKILL needed | `docker start` | docker · L3 |
| C-002 | Hard kill (`docker kill`) — no cleanup at all | exit code 137 | `docker start` | docker · L3 |
| C-003 | **Pause/unpause** (container freezer) — the tree stalls, the TCP stack still ACKs | `State.Paused=true`; no app progress while paused | unpause | docker · L3 |
| C-004 | **OOM-kill** inside the container's cgroup | `State.OOMKilled=true` | restart with the original limit | docker · L3 |
| C-005 | **Network detach/attach** (container loses its network mid-request) | `NetworkSettings` shows no endpoint; target sees `ENETUNREACH` | re-attach; assert reconnect | docker · L3 |
| C-006 | **Volume/bind-mount disappears** mid-write (the cloud volume-loss shape) | path `ENOENT` inside the container while the mount record is gone | remount; assert recovery | docker · L3 |
| C-007 | **Read-only rootfs** (write to `/tmp` or `/var` fails) | `EROFS` in the container | restore | docker · L3 |
| C-008 | **Image pull failure / registry 5xx** — deploy-time recovery | pull error recorded; container absent | registry responds; pull succeeds | sim/proxy · L4 |
| C-009 | **Rolling replacement with connection drop** (new container, same name/IP, old connections killed) — the "replaced under a live client" class | client connections reset at swap time; `StartedAt` changed | none (that is the point) — grade recovery | docker · L3 |
| C-010 | **Healthcheck forced failure** (probe returns 500) — restart-on-unhealthy loops | healthcheck failures ≥ threshold; restart count grows | restore the probe | proxy/docker · L3 |
| C-011 | **Node drain / cordon shape** (faults that assume the node goes away) | simulated scheduler marks the node unschedulable | uncordon | sim · L4 |
| C-012 | **PID-1 reaping gone** (entrypoint that ignores orphaned children) — zombie accumulation | zombie count grows in the container | restart with a real init | docker · L3 |

---

## I — Infra, external services & cloud shapes (14)

Simulator-first: every entry here has a `mode: sim` (default) and, where a real provider
adapter exists (v0.2+), a `mode: real` that is opt-in, allowlisted and spend-capped.
`destroy_class=permanent` never runs outside the simulator in v0.1.

| ID | fault / what it breaks | landed-proof | inverse | Cap |
|---|---|---|---|---|
| I-001 | **Instance/machine restart** (soft then hard) — boot ordering, re-attach, cold caches | boot id changed; uptime reset; service records a fresh start | ensure it boots; assert service healthy | sim/docker · L4 |
| I-002 | **Dependency restart / cold return** — a service returns as an EMPTY instance on the same address (the TRBL-031 shape) | the dependency is up but its state is empty (keys/streams/groups gone) | restore state or re-init consumers; grade recovery | docker/sim · L3/L4 |
| I-003 | **API rate limit** (429 bursts, throttle headers, quota exhaustion) with and without `Retry-After` | provider-sim request log shows rejected calls; the client's retry log | quota restored | sim · L4 |
| I-004 | **API 5xx / malformed response / pagination bug** (same page twice, endless page) | sim log + client's loop counter | sim healthy | sim · L4 |
| I-005 | **Eventual consistency lag** — a write is acknowledged, a read returns the old value for N seconds | the sim serves the stale value for the declared window | window expires; read is fresh | sim · L4 |
| I-006 | **Stale/partial object store** (`404` on a key that exists; truncated multipart; expired presigned URL 403) | sim (or provider) returns that exact status; client's error names it | restore | sim · L4 |
| I-007 | **Volume detach / re-attach** on a live writer (cloud disk loss) | the device disappears from the box; `EIO` observed | re-attach; assert recovery | sim/helper · L4/L2 |
| I-008 | **Floating IP / DNS move** — traffic shifts away (or to a blackhole) | the address no longer answers on the old node | move it back | sim · L4 |
| I-009 | **Instance termination / spot reclaim notice** — the graceful-exit window | termination notice delivered; the node goes away at T+2min | new node; assert state recovery from durable storage | sim · L4 |
| I-010 | **Authentication expiry mid-run** (token/credential expires between requests, key rotated) | the next call 401s while the previous succeeded | re-auth path; assert the client refreshes | sim/proxy · L4 |
| I-011 | **Metadata service fault** (169.254.169.254 timeout/500/stale token) — long credential stalls at boot | metadata requests hang/fail in the sim; the client's boot stalls | sim healthy | sim · L4 |
| I-012 | **Quota/limit exhaustion** (VM count, bucket count, disk quota at the account level) | provider-sim rejects with the quota error | quota freed | sim · L4 |
| I-013 | **Snapshot restore to a stale point** — "recovered" but lost N minutes | restored state's timestamp < the fault time; data delta measurable | (that is the finding) | sim · L4 |
| I-014 | **DNS propagation delay / TTL mismatch** after a change | two resolvers disagree for the declared window | propagate | sim · L4 |

---

## T — Time & clock (4)

Time namespaces are **not available unprivileged on this host** (`unshare --time` →
`EPERM`), so this layer is shim/helper-based in v0.1 and its primitives ship at the
lowest maturity until their selftest is green.

| ID | fault / what it breaks | landed-proof | inverse | Cap |
|---|---|---|---|---|
| T-001 | `clock_gettime`/`gettimeofday` **jumps forward** (e.g. +1 h) — timers fire early, TTLs collapse, leases expire | shim counter + the target's own clock reading | rule removed; clock restored | shim/helper · L1 |
| T-002 | Clock **jumps backward** — monotonic-vs-wall bugs, negative durations, expired cache thinking | as above | as above | shim/helper · L1 |
| T-003 | **Timer acceleration** (time runs 100× fast) — rate limiters, retry budgets, lease churn | shim counter + observed timer frequency | rule removed | shim · L1 |
| T-004 | **Time freeze** (clock stands still) — watchdog never trips, TTL never expires | readings identical across N samples | rule removed | shim/helper · L1 |

---

## Companion faults (deliberately NOT in v0.1)

- **Permanent destruction** (delete an instance, delete a bucket, wipe a volume):
  simulator-only, and refused outside it. The value does not justify the blast radius
  before the reversible set is proven fleet-wide.
- **Kernel-level faults** (kmem / device removal / `MADV_HWPOISON`): the blast radius is
  the host and the recovery path may not exist. Requires a dedicated scratch box as a
  precondition, which is an owner decision, not a primitive.
- **Load/flood faults**: that is a load generator's job; mischief may *accompany* one,
  never own one.
- **Privilege-escalation or exploit delivery**: out of scope permanently.

## Descriptor — the machine-readable form of a row

```yaml
id: S-001
name: write-eio
what: write()/pwrite() to a NAMED path returns EIO
params: {path_substr: str, errno: EIO, budget: int?}
landed_proof: {kind: shim-counter, check: "<run_dir>/shim.<pid>.json hits >= 1"}
inverse: {action: detach-shim-rule, verify: "the next write to the path succeeds"}
capability: none
tier: L1
backend: shim
maturity: proven          # L1-only | proven | fielded
expect_when_healthy: "the failure is recorded and the operation is not acknowledged"
```

Every row in the tables above is publishable in this form; ten of them ship as real
files in `catalog/faults/`, and the four replay cases ship in `catalog/experiments/`.

## Maturity ladder (which primitives may run where)

| rung | requirement | reachable scope |
|---|---|---|
| `L1-only` | no selftest yet | L0 scratch target only |
| `proven` | `mischief selftest --primitive <id>` green on this host + a battery cell exists | its declared tier |
| `fielded` | ≥ 30 days of runs with zero `stuck` faults and zero unproven reverts | any tier, including monkey mode |
