# mischief dogfood diagnostics — how the thing is built and why

## What mischief is, mechanically

At this commit mischief has no binary. Its product is a design contract:
**faults are data with three obligations** — an independent landed-proof, an
inverse recorded before landing, and an enumerable blast radius. Everything in
the repo exists to make those three obligations checkable before any code lands:
the catalog (104 primitives, each row must state all five contract fields or it
"does not enter the catalog"), the spec plan (each spec must falsify the PRD and
carry a NOT-LIST), and the capability probe (measured evidence the backends can
exist on a real Ubuntu host).

## Why the design leans on LD_PRELOAD and scopes

The probe measured (2026-09-27, re-verified 2026-10-02 by the dogfood lane):
- unprivileged cgroup mkdir is DENIED on this host, so cgroup-shaped faults ride
  `systemd-run --user` scopes (L1, rootless) — proven working;
- seccomp user-notify is available (notif=80/resp=24) — the syscall-error backend;
- time namespaces are EPERM unprivileged — hence the T-layer is shim/helper-based
  and shipped at lowest maturity;
- `unshare -Un` fails on Ubuntu AppArmor userns restrictions — network faults
  need the netns+netem path via a narrow sudo allowlist (L2).

That is why the catalog's privilege tiers are L0-L4 and why the flagship
primitive (S-001 write-EIO) is an LD_PRELOAD shim: it needs nothing but the shim.

## The landed-proof idea, observed working

The shim writes `/tmp/mc-fault-hits.<pid>` (`hits=N rule=...`) independently of
the target. During the 2026-10-02 dogfood run: victim's write returned EIO, the
target file stayed 0 bytes, an unaffected fd kept working, and the counter file
existed regardless of what the victim claimed. That counter is the whole
anti-false-green thesis in 28 bytes.

## Errors hit during the run (and the right way)

- `rm -rf` on a scratch path was approval-gated on the harness surface —
  use `mktemp -d` instead of pre-deleting; same evidence, no gate.
- First look for the landed-proof counter file checked the CWD (the probe
  transcript shows it next to the victim); the shim hardcodes `/tmp/`. Read the
  source only when stuck — and that read is itself the docs gap to avoid:
  the shim's counter path is a constant, worth a line in the descriptor.
- seccomp-probe.c emits %zu/int format warnings (cosmetic, filed MSF-027).

## The right way to verify a catalog claim

Never count the table by eye: recompute rows per layer with a script
(`^\|\s*([PSRFNCIT])-\d+\s*\|` per `## X —` section) and compare against the
claimed totals. 2026-10-02 result: claimed 104 = actual 104, no dups, no gaps.
Same discipline the project itself preaches: prove it landed, don't assert it.

## Known honest limitations (by design, not defects)

- `maturity: proven` on some YAMLs currently overstates the ladder (MSF-025) —
  the ladder requires a selftest that cannot exist pre-binary. The ladder is the
  project's honesty mechanism; treat `proven` rows as hand-verified until MSF-025
  closes.
- The four replay experiments are rehearsal artifacts: their selectors
  (`container:trouble-hub`, `process:troubled`, ...) have no resolution semantics
  yet (MSF-026). They document intent; they do not run.

## 2026-10-10 — serve/watchdog run: what the reverter really is

The reverter is not a service you talk to; it is a **fold over an append-only
JSONL journal** whose records ARE the state machine (arm → land → hold_expired
→ revert_proof, plus hold_stuck from boot reconcile). Three consequences a
reader of this file should internalize:

1. The detached TTL owner (systemd-run user service, or a setsid child when the
   user manager is degraded) re-reads inverse + check from the JOURNAL BYTES,
   never from anything the CLI passed in memory. That is why a hold survives
   `kill -9` of the arming process: the owner's orders are already fsynced on
   disk before Land is permitted. The dogfood run verified the daemon-side
   half live: `serve` reverted at expiry with role=owner reason=ttl_expiry and
   a measured `check: same`.
2. Hold ids are content hashes, not counters — the same declaration armed
   twice yields the same id and a second arm line; readers fold duplicates
   into one state. Do not "dedupe" the journal; the evidence IS the lines.
3. Every verdict is measured or it is revert_failed. The run's most valuable
   single observation: an inverse pointing at a missing backup produced
   `outcome=revert_failed detail=inverse failed: read backup: … no such file`,
   exit 1, and the journal kept the fault marked live. A tool that graded that
   "reverted" would be manufacturing exactly the false green this project
   exists to kill.

Gap found this run (DF-05): the state machine has no CLI door — `Lifecycle.Arm`
is reachable only from Go tests. Everything above was driven by hand-writing
one arm record per the fold contract (see 2026-10-10-serve-integration.md for
the exact format). Until an `arm` verb ships, that report is the only working
"how to start a hold" documentation.
