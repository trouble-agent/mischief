# mischief — operator reference: reverter + shim environment variables

Every environment variable mischief reads, one section each: what it does, who
sets it, its default, safe override values, a troubleshooting note, and whether
it is a **supported knob** (documented, stable, safe to set in production) or
**internal-only** (set by the binary itself; do not set it by hand outside
tests/debugging).

**The one-line rule:** if you are arming a fault or running the CLI, you never
need any of these. The CLI derives everything; the variables exist so the
detached owner process and the LD_PRELOAD shim can carry their orders across a
process boundary. Setting them by hand outside tests reproduces what the CLI
already does — and can fight it.

Where the behavior lives: `internal/reverter/owner.go` (reverter variables) and
`internal/backend_signal/rule.go` + `tools/shim-extracted.c` (shim variables).

---

## Reverter: detached TTL owner

The reverter arms a hold, spawns a detached TTL owner (a watchdog that
outlives the CLI — systemd-run transient service when the user manager is
reachable, otherwise a setsid child), and that owner sleeps until the expiry
and performs the measured revert. The first three variables are the **env
fallback channel** for the owner request: the argv flags (`__owner -dir …
-hold … -expiry …`) are primary, the env only fills flags the parser finds
missing. The journal is the durable source of truth — a stale value here can
name *which* hold to act on, but every fact the owner acts on (inverse, check,
fault) is re-read from the journal bytes, not from the env.

### MISCHIEF_REVERTER_DIR — internal-only

- **Purpose:** absolute path to the reverter journal directory. Tells the
  detached owner which journal to open.
- **Who sets it:** the CLI, in `ownerEnvPairs`, when spawning the owner child
  (both detach shapes: systemd-run and setsid).
- **Default / absence:** unset for the CLI process itself. If the owner-side
  parser finds no `-dir` flag and this env is unset, the owner exits with
  `owner: dir missing (flag or MISCHIEF_REVERTER_DIR)`.
- **Safe override:** none. There is no correct hand-set value: the argv flag
  is primary, so an env value is silently ignored whenever the flag is
  present, and a wrong env value with a missing flag points the owner at the
  wrong (or nonexistent) journal. Treat any non-empty value on a production
  process as an anomaly.
- **Troubleshooting:** owner dies immediately with `dir missing` → the argv
  construction failed; check the detach spawn, not this env. Owner opens a
  journal you did not expect → a stale exported value from an earlier shell
  survived into the owner's ambient environment; the owner's env is
  `os.Environ() + ownerEnvPairs`, so leftovers from the arming shell propagate
  into the detached child.

### MISCHIEF_REVERTER_HOLD — internal-only

- **Purpose:** hold ID selector for the owner to act on.
- **Who sets it:** the CLI, in `ownerEnvPairs`, together with DIR/EXPIRY.
- **Default / absence:** unset. Owner-side fallback when `-hold` is absent;
  both missing → `owner: hold missing (flag or MISCHIEF_REVERTER_HOLD)` and
  the owner exits.
- **Safe override:** none (same reasoning as DIR — argv-primary).
- **Troubleshooting:** same stale-ambient-env failure as DIR. Also: if the
  named hold was deleted between spawn and expiry, the owner reports
  `ErrNoHold` ("no live hold for selector") — that is a reported outcome, not
  a crash; check the journal, not the env.

### MISCHIEF_REVERTER_EXPIRY — internal-only

- **Purpose:** wake time for the owner, as a **Unix nanosecond** integer
  (`time.Unix(0, n)` on the owner side). Note the unit: nanoseconds, not
  seconds or milliseconds — a seconds-valued value wakes the owner in the
  distant past.
- **Who sets it:** the CLI, in `ownerEnvPairs`.
- **Default / absence:** unset. Owner-side fallback when `-expiry` is absent;
  both missing → `owner: expiry missing (flag or MISCHIEF_REVERTER_EXPIRY)`.
  Unparseable → `expiry from MISCHIEF_REVERTER_EXPIRY: <parse error>`.
- **Safe override:** none by hand. The expiry is seconds-truncated
  (ceil) into the systemd-run `RuntimeMaxSec` bound, so an env value that
  disagrees with the argv flag is silently ignored; the owner wakes at the
  argv value, not yours.
- **Troubleshooting:** owner exits instantly with an expiry parse error → a
  stale ambient value (seconds instead of ns is the classic) from the arming
  shell. Owner reverted at the "wrong" time → it woke exactly when the argv
  said; audit the arming command, not the env.

### MISCHIEF_REVERTER_DETACH_MODE — internal-only (debug pin)

- **Purpose:** forces the detach shape used to spawn the owner outside the
  CLI's fate. Values: `setsid` → double-fork setsid child re-exec'ing the
  binary's `__owner` branch; `systemd-run` → `systemd-run --user --collect
  --unit=mischief-owner-<hold-id> -p RuntimeMaxSec=<ttl+margin> <self>
  __owner …` (transient service, returns immediately).
- **Who sets it:** tests, and an operator debugging the owner shape.
- **Default:** empty. The CLI probes: with a usable `XDG_RUNTIME_DIR`
  containing a systemd user runtime dir, it uses systemd-run; a degraded user
  session falls through to setsid. The env is a **hint, never a new code
  path** — unknown values are ignored and the natural probe order applies.
  Matching is `TrimSpace`+`ToLower`, so `Systemd-Run ` still pins.
  (Wording note for docs reviewers: the source comment mentions the
  `--scope` form; `detachSystemdRun` actually builds the transient-service
  form because `--scope` runs inside the caller's terminal session and
  deadlocks the arming CLI. The transient-service description above is the
  real behavior; see owner.go's `detachSystemdRun` comment.)
- **Safe override:** `setsid` and `systemd-run` only. `setsid` is the right
  pin when the user manager is degraded or flaky (a mispredicted probe would
  otherwise fall through to setsid late, or fail hard if systemd-run itself
  errors: `ErrNoOwner: systemd-run: …`).
- **Troubleshooting:** `ErrNoOwner: systemd-run: …` on a host with a flaky
  user manager → pin `MISCHIEF_REVERTER_DETACH_MODE=setsid`. Owner visible
  as a `mischief-owner-<hold>` transient unit with `RuntimeMaxSec` bounding
  its life — expected in systemd-run mode; the user manager reaps it, and
  `--collect` GCs the failed unit after exit.

---

## Shim (backend `shim`): rule, budget, proof

The LD_PRELOAD shim intercepts `write` (v0.1 implements exactly that syscall)
and fails matching writes with a named errno. Three variables arm it. The rule
text (`MCFAULT`) is exactly three slots `sys:pattern:errno`, parsed greedily
between the first and last colon so absolute paths with colons work; budget
and proof deliberately travel in their own variables so the three-slot parsing
rule never has exceptions.

### MCFAULT — supported knob

- **Purpose:** the rule text `sys:pattern:errno`. v0.1: `sys` must be
  `write`; `errno` is one of the closed vocabulary `EIO, ENOSPC, EACCES,
  EDQUOT, ENOMEM, EINTR, ETIMEDOUT, EAGAIN, EPIPE` (unknown names are refused
  at parse time — a silently-different fault is a green-but-meaningless run).
  The pattern must be an **absolute** exact path, or an anchored form:
  `dir:*` (prefix: the dir itself or anything under it, path-boundary at
  `/`, never matches `dir.txt.evil`) or `*:name` (suffix: the name or
  anything ending in `/name`, never matches `xcache.tmp`). Bare substrings
  and mixed wildcards (`a*b`) are refused (the MSF-028 contract).
- **Who sets it:** the CLI via `Rule.Env`, prepended with the shim library
  path in `LD_PRELOAD`; reads by the shim's constructor.
- **Default:** none — this variable *arms* the shim. Without `MCFAULT` the
  shim loads inertly (constructor returns early, `loaded=0`, every `write`
  passes through untouched). A rule that fails the Go-side parse never
  reaches a child: arm refuses, or errors name the offending field.
- **Safe override:** rule text of the same shape as the CLI generates. The
  only hand-set scenario worth recommending is inert-by-absence: leaving it
  unset makes the shim harmless. Malformed values fail safe: fewer than 3
  slots, oversized fields, or a syscall other than `write` leave the shim
  loaded but inert (the C constructor's early-return) — note this is the
  *C-side* behavior; the *Go-side* parser refuses with a named error instead,
  so CLI-armed runs never see the inert case silently.
- **Troubleshooting:** target ran but proof counter shows `hits=0` → the
  rule parsed but never matched: wrong path shape (relative pattern refused
  by the Go parser; the C shim would just never match) or the target writes
  through a different fd/path than expected — check `/proc/<pid>/fd` readlink
  targets, which is what the shim matches against. Go-side parse errors name
  the offending slot (`unknown errno`, `unsupported syscall`, `not an
  absolute path`, `mixes a wildcard…`).

### MCFAULT_BUDGET — supported knob

- **Purpose:** how many matching writes may fail. `N` = the first N matching
  writes fail, then writes pass through; `0` = the armed-but-never-fires
  rule (an expressible no-op arm for AC-3-style testing); absent = unlimited
  (-1).
- **Who sets it:** the CLI via `Rule.Env` — omitted entirely when the budget
  is unlimited (negative), emitted as `MCFAULT_BUDGET=N` otherwise.
- **Default:** unlimited (shim `rule_budget = -1`). In the Go API the
  default flows from `ParseRule` (budget -1); `WithBudget(n)` requires
  `n >= 0` — a negative hand-set value is refused Go-side, and the C shim
  only assigns from a non-empty string (`atoi` of garbage is possible
  C-side, another reason hand-setting is a test-only move).
- **Safe override:** `0` to N. `MCFAULT_BUDGET=0` is well-defined
  (armed, never fires; grading reports no_op), useful for verifying the
  arming path without touching the target. Deleting the variable is the
  supported way to get unlimited.
- **Troubleshooting:** fault stopped landing partway through a run → the
  budget was consumed (`hits` reaches `rule_budget`, later writes pass
  through); the proof counter records `hits=N`. Budget seemingly ignored →
  check whether the variable is actually present in the child's env (omitted
  = unlimited by design); `LD_PRELOAD` + `MCFAULT` may also be missing from
  a daemonized child's env if something scrubs the environment across the
  daemonize boundary.

### MCFAULT_PROOF — supported knob

- **Purpose:** the landed-proof counter file. On every hit the shim opens it
  (truncating) and writes `hits=<n> rule=<sys>:<pattern>:<errno>`, so a run
  can distinguish "fault landed" from "fault never landed" — the anti-gaming
  contract (AC-3): Prove grades `no_op`/FAILED when the counter is missing
  or `hits < 1`.
- **Who sets it:** the CLI: `ArmShim` derives it
  (`DeriveProofPath`: `sha256(canonical rule text)`, hex, first 16 chars,
  file `mc-proof-<hash16>.jsonl` in the run dir) and the derived path MUST
  travel in the env — a counter-less shim cannot prove anything.
- **Default:** derived per rule (above). Note the implication: two different
  rules never share a counter; the same rule is idempotent.
- **Safe override:** only inside a test where you also control the grading
  side: point it at a scratch path **outside the rule's own pattern** — the
  proof path is always self-excluded (a write to the proof path never
  matches the rule, whatever the pattern says), but ArmShim refuses to arm
  when a hand-set proof path matches the rule's own pattern (MSF-028 guard).
  Never point it at a file the target might also write.
- **Troubleshooting:** `no shim counter file with hits >= 1 at <path>` → the
  fault never landed (or the proof path was scrubbed from the child's env);
  that verdict is a FAILED no_op, by design — fix the arm, don't bypass the
  check. Counter shows hits but the target seems unaffected → re-read the
  rule: the shim fails `write()` with the named errno; a target that treats
  EIO as retryable may survive by design, which is the point of the verdict.

---

## Related variables (not mischief's, listed for completeness)

- `LD_PRELOAD` — the standard dynamic-loader variable; mischief sets it to
  the shim library path (found among standalone candidates or built into the
  run dir; no library + unbuilt = `capability_unavailable` refusal rather
  than an unfaulted run). Listed here because it travels in the same `Env()`
  block; mischief does not read it itself.
- `XDG_RUNTIME_DIR` — read by the reverter's probe (`systemdUserReachable`)
  to decide the detach shape; not written by mischief. A stale/wrong value
  skews the probe toward systemd-run and surfaces as `ErrNoOwner: systemd-run: …`
  on hosts where the dir exists but the manager is broken — pin with
  `MISCHIEF_REVERTER_DETACH_MODE=setsid`.

## Provenance

Source of truth for every claim above:
`internal/reverter/owner.go` (env constants, `defaultDetach`, `detachSystemdRun`,
`detachSetsid`, `ownerEnvPairs`, `ParseOwnerArgs`), `internal/backend_signal/rule.go`
(`Rule`, `ParseRule`, `WithBudget`, `Env`, `DeriveProofPath`),
`internal/backend_signal/shim.go` (`ArmShim`, `Prove`), and
`tools/shim-extracted.c` (shim constructor, budget/proof handling, match logic).
