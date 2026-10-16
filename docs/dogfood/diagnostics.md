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

### 2026-10-16 arc closure — the arm verb shipped and the CLI door is real

The DF-05 gap above closed on 2026-10-14: commit 8137f85 shipped `mischief arm`
(cmd/mischief/arm.go), the CLI write-ahead surface, and the README gained a
dedicated "Arming a hold" section. The 2026-10-16 dogfood run 15 drove it as a
real consumer end-to-end — sanctioned arm → status → hand-landed fault →
`serve` TTL revert (`check: same`, byte-restored target) → `revert --all`
kill switch (remove-file inverse, `check: absent`, 4 ms wall) → boot reconcile
grading an expired hold it never saw armed. The hand-written-record recipe in
2026-10-10-serve-integration.md is now obsolete for everyday use: `arm` writes
the same journal shape (verified byte-level — the record carries fault_id,
target, both decls, ttl in ns, boot_id, arm_pid) and refuses malformed params
with the offending pair named. What the CLI door did NOT fix: `Hold.Validate`
checks structure only, so an unknown inverse/check KIND still journals fine and
surfaces only as a later `revert_failed` (MISCHIEF-DF-29) — the doc comment in
arm.go promises arm-time refusal that the code does not implement. Until that
closes, treat arm's kind flags as the reverter's closed vocabulary and never
invent kinds: the verb will accept them and the watchdog will refuse them.

### 2026-10-16b arc — the release consumer: what the tarball path really costs

Run 16b (lane mischief-chaos-dogfood) drove the artifact a non-Go user actually
gets: the released v0.1.0 tarball through README Option A, verbatim, on a bare
fresh agent. What the mechanical path proves (all held): download + the
arm64-grep sha256 discipline, the four documented archive files, `version`
stamping v0.1.0 (tag-stamped, unlike `make bin`'s sha stamp), `status` folding
from a cold run dir, `doctor` failing with exactly the message the README
warns about, and plan's full preview (0.01 s). The friction is all in the
connecting tissue the README under-specifies, and each one cost a real stall:

1. `install -m 0755 mischief ~/.local/bin/` assumes `~/.local/bin` exists;
   on a bare Debian user it does not, so the quickstart dies rc=1 and the
   three PATH smoke verbs die command-not-found until a `mkdir` rescue
   (MISCHIEF-DF-36). Dev-host coverage could never see this: the dir always
   pre-exists there.
2. The Option A walkthrough says "plan creates a run dir and journal entry";
   plan journals nothing — by construction, per its own doc comment
   (internal/plan/plan.go:13), and per the sibling README line ("zero side
   effects, AC-1"). The README contains both truths; the walkthrough clause
   is the false one (MISCHIEF-DF-37). Lesson for readers: grep the run dir
   after a plan run, trust the run_id on stdout, and do not conclude records
   were lost.
3. Catalog vintage is version-gated in selftest: the released binary's
   selftest corpus is the tag's 10 fault files; a HEAD catalog (11 files,
   F-008 added after the Oct 7 tag) fails the L0 leg with `unknown primitive
   "F-008": not in the selftest corpus` (MISCHIEF-DF-38). The remedy the
   README itself suggests ("run from a checkout") is exactly what builds
   that mismatch — the proven pairing on the agent was release binary +
   `git archive v0.1.0 catalog` → rc=0. The failure direction is correct
   (fail-closed on an unproven primitive); what is missing is a version-skew
   hint in the message.

Right way to dogfood the release artifact on a fresh box (works verbatim):
download tarball + checksums.txt, `sha256sum -c` via the arch-specific grep,
untar, `mkdir -p ~/.local/bin` then `install -m 0755`, put the shipped
`libfault-anchored.so` at `/tmp/mischief-shim/` (the README's one prebuilt
location), for catalog verbs stage the matching TAG's catalog tree, and run
the L1 sanction recipe with a reason-bearing marker file. Timings on a bare
agent: download 0.53 s + 0.45 s, selftest cold 0.53-0.65 s, plan 0.01 s.
