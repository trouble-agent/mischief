// Package reverter is SPEC-04: the write-ahead inverse, TTL ownership, the
// detached reverter, boot reconcile, and the kill switch.
//
// Design authority: docs/SPEC-PLAN.md (SPEC-04 reverter & lifecycle) and
// docs/prd/mischief-v0.1.md §5 ("The inverse is recorded (write-ahead)
// *before* the fault lands; a separate process owns the TTL and reverts even
// if the CLI is `kill -9`ed. Boot reconcile marks leftover faults `stuck` ...
// Kill switch `mischief revert --all` — proven reverted, not merely issued")
// and AC-4/AC-5/AC-6.
//
// The lifecycle, in the order the package enforces it:
//
//   - Arm (write-ahead): the hold — fault, inverse, measured check, TTL — is
//     appended to the journal and fsynced BEFORE the caller may land the
//     fault. Land refuses (and records land_refused) when the write-ahead
//     line is not on disk: the inverse always predates the fault.
//   - Detach: a TTL owner process is spawned OUTSIDE the CLI's fate — the
//     proven shape `systemd-run --user --scope -p RuntimeMaxSec=<n>` when the
//     user manager is reachable, otherwise a real setsid child. The owner
//     re-executes this package's own owner entry (OwnerMain) and reverts at
//     TTL expiry no matter what the CLI did.
//   - Revert (measured, AC-4): the inverse is executed from the journal
//     record and the affected resource is PROBED afterwards. A revert whose
//     check fails — or whose inverse cannot execute at all — records
//     outcome=revert_failed with the measured detail, never "reverted".
//   - Boot reconcile: at Open, any non-terminal hold from a boot other than
//     the current one is marked hold_stuck (reason previous_boot) and shows
//     in Status; the kill switch still reverts it.
//   - Kill switch (AC-6): RevertAll reverts every live or stuck hold within
//     the wall budget (2s) and appends one revert_proof per fault — which
//     fault, the measured check, the outcome, the duration.
//
// State is an append-only JSONL journal under Dir (default ~/.mischief/runs/
// default, the same run dir the CLI verbs arm/status/revert/serve use via
// --dir; library callers get it via DefaultDir when Open is called with an
// empty dir), plus one boot.json identity marker (host boot id → reverter
// boot id) that makes "previous boot" a DISK fact, not a process generation:
// every process on one host boot shares the marker's reverter boot id, a new
// host boot rotates it. Record lines are never rewritten in place; state
// changes are new lines; readers fold the log. Hold ids are content-
// derived hex(sha256)[:16] following internal/journal's run-id convention —
// a pure function of the hold's declared content, never of a clock. Every
// record is scrubbed on write with internal/journal's rule set (the rules
// are mirrored here because the journal package's are unexported and this
// package must not edit files outside its own boundary).
//
// Glossary (names owned here, reused by the CLI):
//
//   - Hold: one armed fault with its write-ahead inverse, check and TTL.
//   - Inverse: the recorded undo — declarative (action + params) so a
//     DETACHED process can execute it from journal bytes alone.
//   - Check: the measured post-revert probe (declarative, same reasoning).
//   - Proof: the revert_proof record — fault id, outcome, measured detail,
//     duration, who reverted, which pid.
//   - Stuck: a non-terminal hold inherited from a previous boot.
//   - Kill switch: RevertAll("kill_switch") — everything, proven.
//
// TTL clock model (MISCHIEF-DF-59): what "TTL 30s" means, exactly.
//
// Journal records carry whole-second timestamps (rec.TS is a floor of the
// arm instant), so a hold's expiry is derived on the SECOND GRID, one second
// beyond the last possible arm instant: arm at T+0.9s with TTL 30s → expiry
// at T+31s. A hold therefore NEVER reverts before its advertised TTL has
// fully elapsed (that would be a shorter containment than declared); the
// cost is a bounded overshoot of at most 1s on the derived boundary. The
// detached owner reverts at the nanosecond expiry passed at Detach time —
// this same second-grid value. Daemon-side (mischiefd, internal/daemon),
// expiration is additionally QUANTIZED TO THE 5s TICK (HeartbeatInterval):
// a hold whose expiry passed mid-tick is reverted at the next tick, and
// health.json's ttl_remaining decays per tick — so the daemon path reverts
// within (up to) 5s AFTER the derived expiry, never earlier. Both delays
// are on the LATE side by design: an early revert is a contract break, a
// late one is documented latency.
//
// NOT-LIST (what SPEC-04 does not promise here):
//
//   - No actuators: landing a fault is the caller's land function (SPEC-06/07
//     own the primitives); this package owns the lifecycle around it.
//   - The closed inverse/check action set is file-domain only (restore-file,
//     remove-file; file-matches-backup, file-absent) — enough for v0.1's L0
//     proofs, extensible without changing the record shape. An UNKNOWN check
//     kind grades revert_failed (fail closed): a revert that cannot be
//     measured is not a revert.
//   - No /health.json: the daemon surface (mischiefd) serves it; this
//     package provides the state (Status) it will read.
//   - The owner survives kill -9 of the CLI, not kill -9 of ITSELF, and not
//     a host power loss mid-revert (the journal proof makes the gap
//     visible; a boot-reconcile + kill switch closes it).
//
// ch:trace row=MSF-005 spec=docs/SPEC-PLAN.md (SPEC-04) + docs/prd/mischief-v0.1.md (§5, §7, AC-4/5/6) evidence=internal/reverter/ witness=none:no-live-host-run-in-worktree
package reverter
