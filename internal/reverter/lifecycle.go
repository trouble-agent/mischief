package reverter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// ErrNoHold is returned when a selector names a hold that does not exist or
// is already terminal (a revert of nothing is reported, not swallowed — the
// no_op shape is a report, never a pass).
var ErrNoHold = errors.New("reverter: no live hold for selector")

// ErrWriteAheadMissing is Land's refusal when the hold's write-ahead line is
// not durable: the inverse would not predate the fault, so landing is
// refused and recorded (land_refused).
var ErrWriteAheadMissing = errors.New("reverter: write-ahead inverse not durable — land refused")

// ErrNoOwner is returned when Detach cannot spawn an out-of-process owner
// (neither systemd-run nor the setsid fallback could start). The arm stays
// in-process: the CLI must not pretend a detached owner exists.
var ErrNoOwner = errors.New("reverter: detached TTL owner could not be started")

// ErrInverseUnreadable is Arm's refusal when the inverse's undo cannot
// possibly execute: a restore-file inverse reads its backup at revert time,
// so a backup that is not readable AT ARM TIME can only ever grade
// revert_failed later (MISCHIEF-DF-23 — the arm side refuses the hold up
// front instead of booking a live un-reverted fault at TTL expiry). The
// error names the unreadable path; refusals precede the write-ahead line.
var ErrInverseUnreadable = errors.New("reverter: arm: inverse restore-file backup is not readable")

// ErrNotRetryable is RevertRetry's refusal when the hold is not in the ONE
// state a retry exists for: a terminal measured revert_failed (MISCHIEF-
// DF-52). A live (armed/stuck) hold belongs to the plain revert path, and a
// hold already measured `reverted` has nothing left to undo — appending a
// second proof for it would be a fabricated revert. The error names the
// status; nothing is written.
var ErrNotRetryable = errors.New("reverter: --retry requires a hold whose status is revert_failed")

// Lifecycle is the reverter API: arm → land → (hold) → measured revert, with
// TTL ownership and the kill switch. All methods are safe for concurrent
// use; all durable state is the journal.
type Lifecycle struct {
	store *Store
	// exec is the inverse/check executor (the default uses /bin/sh; tests
	// inject their own Decl kinds and executor — no network, no root).
	exec executor
	// detach is the owner-spawn seam (default: real setsid child or
	// systemd-run). Tests override it to observe the spawn shape; the
	// kill -9 test does NOT override it (a real forked child is required).
	detach func(o ownerRequest) (ownerResult, error)
}

// executor is the inverse/check execution seam.
type executor interface {
	execAction(kind string, d Decl) (string, error)
	evalCheck(kind string, d Decl) (detail string, ok bool, err error)
}

// ownerRequest is one detached-owner spawn request.
type ownerRequest struct {
	// Dir is the journal dir the owner watches.
	Dir string
	// HoldID is the hold this owner owns the TTL for.
	HoldID string
	// Expiry is the absolute TTL expiry (unix nanos) the owner reverts at.
	Expiry int64
	// Reason is the revert reason the owner records ("ttl_expiry").
	Reason string
	// RuntimeMaxSec bounds the owner's lifetime (TTL + margin, ceil).
	RuntimeMaxSec int64
}

// ownerResult reports how the owner was spawned.
type ownerResult struct {
	// Mode is "systemd-run" or "setsid".
	Mode string
	// PID is the owner process's pid (the setsid child's pid; 0 for the
	// systemd-run shape, where the USER MANAGER owns the process — the
	// unit name is the handle).
	PID int
	// Unit is the systemd unit name (systemd-run shape only, "" otherwise).
	Unit string
}

// New builds a Lifecycle over store.
func New(store *Store) *Lifecycle {
	return &Lifecycle{store: store, exec: scratchExec{}, detach: defaultDetach}
}

// ValidateUndo is the exported arm-side undo probe: for a restore-file
// inverse it performs the SAME read the executor performs at revert time
// (scratchExec.execAction's os.ReadFile of the backup), so a caller that
// pre-checks the hold cannot drift from what the undo will actually do. An
// absent backup param is the same unreadable class (a paramless restore
// touches nothing). Kinds other than restore-file are outside the probe
// (nil: unknown kinds are graded by exec.go at revert, fail-closed there —
// the CLI's Hold pre-validation catches them first).
func ValidateUndo(d Decl) error {
	return validateInverseUndo(d)
}

// validateInverseUndo is the arm-side probe behind Arm's refusal: for a
// restore-file inverse it performs the SAME read the executor performs at
// revert time (scratchExec.execAction's os.ReadFile of the backup), so the
// arm-time verdict cannot drift from what the undo will actually do. An
// absent backup param is the same unreadable class (a paramless restore
// touches nothing). Every not-delivered outcome returns a non-nil error
// naming the path; kinds other than restore-file are outside the probe
// (unknown kinds are graded by exec.go at revert, fail-closed there).
func validateInverseUndo(d Decl) error {
	if d.Kind != "restore-file" {
		return nil
	}
	backup := d.Params["backup"]
	if backup == "" {
		return fmt.Errorf("%w: missing backup param (a restore-file inverse needs one)", ErrInverseUnreadable)
	}
	if _, err := os.ReadFile(backup); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInverseUnreadable, backup, err)
	}
	return nil
}

// Arm is the write-ahead half of the contract: the hold — fault, inverse,
// measured check, TTL — is appended to the journal and fsynced BEFORE the
// caller may land the fault. The returned hold id is content-derived
// (hex(sha256)[:16]); arming the same declaration twice yields the same id
// and a second arm line (the fold collapses duplicate states, not evidence).
// Before anything is written, the inverse's undo is probed for executability
// (validateInverseUndo): a hold whose restore backup is not readable is
// refused with the path named — the arm side does not book a fault whose
// undo can only grade revert_failed (MISCHIEF-DF-23).
func (l *Lifecycle) Arm(h Hold) (string, error) {
	if err := h.Validate(); err != nil {
		return "", fmt.Errorf("reverter: arm: %w", err)
	}
	if err := validateInverseUndo(h.Inverse); err != nil {
		return "", err
	}
	l.store.mu.Lock()
	now := l.store.Now()
	id := h.ID()
	s := l.store
	rec := record{
		Type:   RecordArm,
		HoldID: id,
		TS:     now.Unix(),
		BootID: s.BootID,
		Payload: &armPayload{
			FaultID: h.FaultID,
			Target:  h.Target,
			Inverse: h.Inverse,
			Check:   h.Check,
			TTL:     durationNanos(h.TTL),
			BootID:  s.BootID,
			ArmPID:  os.Getpid(),
		},
	}
	b, err := rec.encode()
	if err != nil {
		l.store.mu.Unlock()
		return "", fmt.Errorf("reverter: arm: %w", err)
	}
	line := make([]byte, 0, len(b)+1)
	line = append(line, b...)
	line = append(line, '\n')
	if err := s.appendLocked(line); err != nil {
		l.store.mu.Unlock()
		return "", fmt.Errorf("reverter: arm: %w", err)
	}
	l.store.mu.Unlock()
	return id, nil
}

// Land records that the caller landed the fault, and ONLY then returns
// permission-shaped success: Land first verifies the hold's write-ahead arm
// line is durable in the folded state (it must be: Arm fsynced before
// returning — the check pins the ordering the journal proves). A hold with
// no durable arm record is refused (and the refusal recorded), so the
// inverse always predates the fault.
func (l *Lifecycle) Land(holdID string) error {
	s := l.store
	s.mu.Lock()
	_, durable := s.armed[holdID]
	s.mu.Unlock()
	if !durable {
		now := s.Now()
		_ = s.append(record{
			Type:   RecordLandRefused,
			HoldID: holdID,
			TS:     now.Unix(),
			BootID: s.BootID,
			Fields: map[string]string{"reason": "write-ahead arm line absent"},
		})
		return ErrWriteAheadMissing
	}
	now := s.Now()
	return s.append(record{
		Type:   RecordLand,
		HoldID: holdID,
		TS:     now.Unix(),
		BootID: s.BootID,
		OwnPID: os.Getpid(),
		Fields: map[string]string{"landed": "1"},
	})
}

// Revert executes the hold's recorded inverse, then MEASURES (AC-4): the
// check decl runs against the affected resource and the measured result —
// pass or fail — is recorded in a revert_proof. A failed action, a failed
// check or an unmeasurable check yields outcome=revert_failed with the
// measured detail, NEVER "reverted". The reason names why the revert ran
// (ttl_expiry / kill_switch / direct). A selector with no live hold returns
// ErrNoHold and writes nothing (a revert of nothing is reported, not
// swallowed). A hold that is already terminal is refused here — for a
// terminal revert_failed hold the refusal names the retry entry point
// (RevertRetry, the `revert --retry` flag), which is the only path allowed
// to re-execute a recorded inverse on a terminal hold.
func (l *Lifecycle) Revert(ctx context.Context, holdID, reason string, role Role) (Proof, error) {
	if !role.Valid() {
		return Proof{}, fmt.Errorf("reverter: revert: role %q is not in the closed vocabulary", role)
	}
	s := l.store
	s.mu.Lock()
	st, ok := s.holds[holdID]
	if !ok {
		s.mu.Unlock()
		return Proof{}, fmt.Errorf("%w: %s", ErrNoHold, holdID)
	}
	status := st.Status
	terminal := status == StatusReverted || status == StatusRevertFailed
	arm := st.Arm
	expiry := st.Expiry
	s.mu.Unlock()

	// TTL ownership: an early manual revert before expiry is allowed
	// (operator kill switch per-fault), but the owner's expiry is the
	// contract for the detached path — passed through in the proof fields.
	_ = expiry

	if terminal {
		if status == StatusRevertFailed {
			// MISCHIEF-DF-52: a failed measured revert is not a dead end.
			// The fault may still be live, so the refusal names the retry
			// entry point (the CLI spells it --retry) instead of only
			// stating the terminal status.
			return Proof{}, fmt.Errorf("%w: %s (already terminal: %s — re-run with --retry to re-execute the recorded inverse)", ErrNoHold, holdID, status)
		}
		return Proof{}, fmt.Errorf("%w: %s (already terminal: %s)", ErrNoHold, holdID, status)
	}

	return l.revertNow(holdID, arm, reason, role)
}

// RevertRetry re-executes the recorded inverse of a hold whose measured
// revert is TERMINAL revert_failed (MISCHIEF-DF-52). The fault such a hold
// names may still be live, so the honest recovery is another MEASURED
// attempt rather than a permanent refusal — the retry runs the SAME
// recorded inverse against the SAME check and is graded by the same AC-4
// rule as any first attempt (a retry that does not measure as reverted
// grades revert_failed again, loudly).
//
// The retry APPENDS a new revert_proof: the journal is append-only, so the
// failed attempt and the retry's outcome are BOTH on the record and Status
// folds the latest outcome into the hold's status. Evidence is never
// rewritten, only added to.
//
// Admission is narrow on purpose — only a revert_failed hold — and every
// other status is refused with ErrNotRetryable, naming the status, having
// written nothing:
//
//   - armed / stuck: the hold is live; the plain revert path (no flag)
//     reverts it, and a "retry" there would be a second revert of the same
//     live fault under a misleading name;
//   - reverted: the fault is already measured undone; a second proof would
//     fabricate a revert that never happened.
func (l *Lifecycle) RevertRetry(ctx context.Context, holdID, reason string, role Role) (Proof, error) {
	if !role.Valid() {
		return Proof{}, fmt.Errorf("reverter: revert: role %q is not in the closed vocabulary", role)
	}
	s := l.store
	s.mu.Lock()
	st, ok := s.holds[holdID]
	if !ok {
		s.mu.Unlock()
		return Proof{}, fmt.Errorf("%w: %s", ErrNoHold, holdID)
	}
	status := st.Status
	arm := st.Arm
	s.mu.Unlock()
	if status != StatusRevertFailed {
		return Proof{}, fmt.Errorf("%w: %s (status is %s)", ErrNotRetryable, holdID, status)
	}
	return l.revertNow(holdID, arm, reason, role)
}

// revertNow is the execution core Revert and RevertRetry share: execute the
// recorded inverse, MEASURE the check against the affected resource, append
// the measured proof (AC-4). The two entry points differ ONLY in the hold
// status they admit — never in how an outcome is measured or recorded, so a
// proof appended by a retry is the same evidence a first attempt produces.
func (l *Lifecycle) revertNow(holdID string, arm armPayload, reason string, role Role) (Proof, error) {
	s := l.store
	start := s.Now()
	_, actionErr := l.exec.execAction(arm.Inverse.Kind, arm.Inverse)
	detail, checkOK, checkErr := l.exec.evalCheck(arm.Check.Kind, arm.Check)
	dur := s.Now().Sub(start)

	outcome := OutcomeReverted
	switch {
	case actionErr != nil:
		outcome = OutcomeRevertFailed
		if detail == "" {
			detail = "inverse failed: " + actionErr.Error()
		} else {
			detail = fmt.Sprintf("inverse failed: %v; check: %s", actionErr, strings.TrimSpace(detail))
		}
	case checkErr != nil:
		outcome = OutcomeRevertFailed
		detail = fmt.Sprintf("inverse ok; check error: %v; check: %s", checkErr, strings.TrimSpace(detail))
	case !checkOK:
		outcome = OutcomeRevertFailed
		detail = "inverse ok; check FAILED: " + strings.TrimSpace(detail)
	default:
		detail = "check: " + strings.TrimSpace(detail)
	}

	proof := Proof{
		HoldID:   holdID,
		FaultID:  arm.FaultID,
		Target:   arm.Target,
		Outcome:  outcome,
		Detail:   strings.TrimSpace(detail),
		Duration: dur,
		Role:     role,
		PID:      os.Getpid(),
		Reason:   reason,
	}
	if err := s.append(proofRecord(proof, s.BootID)); err != nil {
		return proof, fmt.Errorf("reverter: revert %s: %w", holdID, err)
	}
	return proof, nil
}

// proofRecord builds the revert_proof journal record for a measured proof.
// The boot id is the PROVER's boot id (passed by the caller — the reverter
// must not derive state from a global when the caller owns the store).
func proofRecord(p Proof, bootID string) record {
	return record{
		Type:    RecordRevertProof,
		HoldID:  p.HoldID,
		TS:      time.Now().Unix(),
		BootID:  bootID,
		OwnPID:  p.PID,
		Outcome: p.Outcome,
		Fields: map[string]string{
			"fault_id":       p.FaultID,
			"target":         p.Target,
			"detail":         p.Detail,
			"role":           string(p.Role),
			"reason":         p.Reason,
			"duration_nanos": fmt.Sprintf("%d", int64(p.Duration)),
		},
	}
}

// Detach spawns the detached TTL owner for one hold: a process OUTSIDE the
// CLI's fate that reverts the hold at expiry (AC-5). Shape order on the
// natural path: the liveness-verified `systemd-run --user --collect
// --unit=<name> -p RuntimeMaxSec=<ttl+margin>` when the user manager is
// reachable (a dead/unobservable unit falls back to the setsid shape),
// otherwise a real setsid child re-execs OwnerMain. The owner survives
// kill -9 of the CLI; it reverts from the journal bytes alone.
func (l *Lifecycle) Detach(holdID string, ttl time.Duration) (ownerResult, error) {
	s := l.store
	s.mu.Lock()
	st, ok := s.holds[holdID]
	var expiry time.Time
	if ok {
		expiry = st.Expiry
	}
	s.mu.Unlock()
	if !ok {
		return ownerResult{}, fmt.Errorf("%w: %s", ErrNoHold, holdID)
	}
	margin := ttl / 4
	if margin < 5*time.Second {
		margin = 5 * time.Second
	}
	if margin > 60*time.Second {
		margin = 60 * time.Second
	}
	// RuntimeMaxSec is seconds-truncated by division; ceil it so the scope
	// never expires BEFORE the TTL it owns (a scope that kills the owner
	// early would strand the hold unowned).
	maxSec := int64(math.Ceil(float64(time.Until(expiry)+margin) / float64(time.Second)))
	if maxSec < 1 {
		maxSec = 1
	}
	req := ownerRequest{
		Dir:           s.Dir,
		HoldID:        holdID,
		Expiry:        expiry.UnixNano(),
		Reason:        "ttl_expiry",
		RuntimeMaxSec: maxSec,
	}
	if l.detach == nil {
		l.detach = defaultDetach
	}
	res, err := l.detach(req)
	if err != nil {
		return res, err
	}
	now := s.Now()
	if err := s.append(record{
		Type:   RecordHoldExpired,
		HoldID: holdID,
		TS:     now.Unix(),
		BootID: s.BootID,
		OwnPID: res.PID,
		Fields: map[string]string{"mode": res.Mode, "expiry_unix": fmt.Sprintf("%d", expiry.Unix())},
	}); err != nil {
		return res, fmt.Errorf("reverter: detach: %w", err)
	}
	return res, nil
}

// RevertAll is the kill switch (AC-6): revert every live or stuck hold
// within wall (2s), with a per-fault proof entry — which fault, the measured
// check, the outcome, the duration. Returns the proofs in hold-id order
// (deterministic) and the wall duration the sweep took. A hold whose
// measured revert fails does NOT stop the sweep: the kill switch reports
// revert_failed for it and keeps going (every fault gets its proof either
// way).
func (l *Lifecycle) RevertAll(ctx context.Context, reason string) ([]Proof, time.Duration, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	s := l.store
	s.mu.Lock()
	ids := make([]string, 0, len(s.holds))
	for id, st := range s.holds {
		switch st.Status {
		case StatusArmed, StatusStuck:
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	s.mu.Unlock()

	proofs := make([]Proof, 0, len(ids))
	for _, id := range ids {
		if ctx.Err() != nil {
			return proofs, time.Since(start), ctx.Err()
		}
		pr, err := l.Revert(ctx, id, reason, RoleKillSwitch)
		if err != nil {
			// A proof write that failed is worse than a failed revert: the
			// sweep aborts loudly (the journal is the evidence contract).
			return proofs, time.Since(start), err
		}
		proofs = append(proofs, pr)
	}
	return proofs, time.Since(start), nil
}

// Status is the state surface (status verb + future /health.json): the
// active holds (armed + TTL remaining), the stuck leftovers (previous
// boots), and the reverter owner pids.
//
// The --json form is NOT this struct's field names: MarshalJSON renders it
// through statusWire below (MISCHIEF-DF-53) so the machine surface is
// snake_case like every other JSON surface in the product (plan, doctor,
// audit, the daemon's health.json) and an unset timestamp is null instead
// of time.Time's "0001-01-01T00:00:00Z". The domain fields stay time.Time
// so the daemon's TTL math (Expiry.IsZero()/Sub) is untouched.
type Status struct {
	// BootID is this process generation's id.
	BootID string
	// Holds is the ACTIVE holds only: armed or stuck, with TTL remaining
	// for live ones. A terminal hold (reverted / revert_failed) owes no
	// inverse — it is a recorded FACT, so it stays in History and in the
	// proof count and never reads as a live hold. The complete listing is
	// StatusAll (`status --all`).
	Holds []HoldStatusEntry
	// RevertFailedHolds is the terminal holds whose LATEST measured revert
	// is revert_failed (MISCHIEF-DF-54-R2): the fault such a hold names may
	// STILL be live — the same condition that flips the daemon's
	// health.json ok:false — so every status surface (text, --json, the
	// exit code) derives from THIS one field, folded from the same journal
	// state the listing is, and the CLI can never disagree with the daemon
	// about a possibly-live fault. A successful retry re-folds the hold to
	// reverted, which empties this set (the fold keeps the latest outcome).
	RevertFailedHolds []HoldStatusEntry
	// History is every measured proof ever appended to the journal
	// (insertion order).
	History []Proof
}

// MayBeLive reports whether the fold carries a fault that may still be
// live: one or more terminal revert_failed holds. This is the ONE
// liveness rule the CLI's exit codes, the text counter and the JSON
// counter all read — the daemon's ok:false flip (internal/daemon/daemon.go)
// derives from the same fold's revert_failed rows (StatusAll), so both
// surfaces answer the same fold the same way.
func (s Status) MayBeLive() bool { return len(s.RevertFailedHolds) > 0 }

// HoldStatusEntry is one hold's Status row. Marshalled through
// holdStatusWire (MISCHIEF-DF-53): Expiry/StuckAt are pointers on the wire
// so unset renders null — a json tag alone cannot do that for time.Time.
type HoldStatusEntry struct {
	// HoldID is the content-derived hold id.
	HoldID string
	// FaultID is the declared fault id.
	FaultID string
	// Target is the declared target selector.
	Target string
	// Status is the hold's lifecycle status (closed vocabulary).
	Status HoldStatus
	// Expiry is the hold's absolute TTL expiry (zero when unknown).
	Expiry time.Time
	// StuckAt is when boot reconcile marked the hold stuck (zero when live).
	StuckAt time.Time
	// LandPID is the pid that landed the fault (0 = never landed).
	LandPID int
	// OwnPID is the pid that last proved this hold (0 = none).
	OwnPID int
}

// ── status --json wire contract (MISCHIEF-DF-53) ─────────────────────────────
//
// statusWire is the ONE document `mischief status --json` emits. The mirrors
// exist because a json tag alone cannot render an unset time.Time as null
// (time.Time.MarshalJSON always writes year 1) and the domain structs must
// not switch to *time.Time (that would push nil-checks into the daemon's TTL
// math) — so the pointers live here only.
//
// cmd/mischief/status_json_test.go enumerates the exact key surface of every
// level (top-level, one hold row, one proof row): a field added to Status /
// HoldStatusEntry / Proof without a mirror key fails that test loud, which is
// the point of keeping the mirrors in one block.

// statusWire mirrors Status. RevertFailedHolds carries the same rows the
// terminal revert_failed rows are (MISCHIEF-DF-54-R2) — the machine
// surface's liveness signal, present at the top level so a consumer
// answers "may a fault still be live?" from ONE key instead of folding
// history themselves. Holds is always a non-nil slice so the healthy
// state renders [] instead of the pre-fix null.
type statusWire struct {
	BootID            string           `json:"boot_id"`
	Holds             []holdStatusWire `json:"holds"`
	RevertFailedHolds []holdStatusWire `json:"revert_failed_holds"`
	History           []proofWire      `json:"history"`
}

// holdStatusWire mirrors HoldStatusEntry. Expiry/StuckAt are pointers so an
// unset timestamp is null; the key is always present, so a consumer reads
// `.holds[].stuck_at` without a missing-key branch. Expiry is NULL for any
// non-active row (MISCHIEF-DF-54-R2): the row's arm-time expiry would
// otherwise render as a future-looking countdown (the residual the text
// row's ttl_remaining gate removed in DF-54) — the wire keeps the key (the
// key set stays fixed) but nulls the value, so `expiry - now` can never
// read "still live" for a finished hold.
type holdStatusWire struct {
	HoldID  string     `json:"hold_id"`
	FaultID string     `json:"fault_id"`
	Target  string     `json:"target"`
	Status  HoldStatus `json:"status"`
	Expiry  *time.Time `json:"expiry"`
	StuckAt *time.Time `json:"stuck_at"`
	LandPID int        `json:"land_pid"`
	OwnPID  int        `json:"own_pid"`
}

// proofWire mirrors Proof. DurationNanos keeps the journal record's own field
// name (internal/reverter/record.go writes duration_nanos), so the raw
// journal and the status surface spell it the same way.
type proofWire struct {
	HoldID        string  `json:"hold_id"`
	FaultID       string  `json:"fault_id"`
	Target        string  `json:"target"`
	Outcome       Outcome `json:"outcome"`
	Detail        string  `json:"detail"`
	DurationNanos int64   `json:"duration_nanos"`
	Role          Role    `json:"role"`
	PID           int     `json:"pid"`
	Reason        string  `json:"reason"`
}

// MarshalJSON renders Status as the snake_case wire document the status verb
// emits (MISCHIEF-DF-53). Every slice is non-nil on the wire: an empty fold
// renders [] (not the null a nil slice marshalles to — MISCHIEF-DF-54-R2,
// `jq '.holds[]'` must iterate instead of crashing), so a consumer never
// needs a null branch.
func (s Status) MarshalJSON() ([]byte, error) {
	holds := make([]holdStatusWire, 0)
	for _, h := range s.Holds {
		hold, err := holdWire(h)
		if err != nil {
			return nil, err
		}
		holds = append(holds, hold)
	}
	rf := make([]holdStatusWire, 0)
	for _, h := range s.RevertFailedHolds {
		row, err := holdWire(h)
		if err != nil {
			return nil, err
		}
		rf = append(rf, row)
	}
	history := make([]proofWire, 0)
	for _, p := range s.History {
		history = append(history, proofWire{
			HoldID:        p.HoldID,
			FaultID:       p.FaultID,
			Target:        p.Target,
			Outcome:       p.Outcome,
			Detail:        p.Detail,
			DurationNanos: int64(p.Duration),
			Role:          p.Role,
			PID:           p.PID,
			Reason:        p.Reason,
		})
	}
	return json.Marshal(statusWire{BootID: s.BootID, Holds: holds, RevertFailedHolds: rf, History: history})
}

// holdWire renders one hold row onto the wire. Expiry is null when the row
// is not ACTIVE (MISCHIEF-DF-54-R2 — a terminal row's arm-time expiry must
// not publish a countdown) or unset; StuckAt keeps the unset-is-null rule.
// The wire's own liveness vocabulary is Status.Active — the one predicate
// the text rows' ttl_remaining gate and the daemon's TTL math already share.
func holdWire(h HoldStatusEntry) (holdStatusWire, error) {
	expiry := wireTime(h.Expiry)
	if !h.Status.Active() {
		expiry = nil
	}
	return holdStatusWire{
		HoldID:  h.HoldID,
		FaultID: h.FaultID,
		Target:  h.Target,
		Status:  h.Status,
		Expiry:  expiry,
		StuckAt: wireTime(h.StuckAt),
		LandPID: h.LandPID,
		OwnPID:  h.OwnPID,
	}, nil
}

// wireTime maps a domain timestamp onto the wire: the zero time is JSON null
// (unset), anything else keeps time.Time's own RFC3339 encoding.
func wireTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// Status folds the current state: the ACTIVE holds only (armed or stuck,
// with TTL remaining for live ones), the boot id, and every proof in
// History. A terminal hold is a recorded FACT, not active state — before
// MISCHIEF-DF-54 this surface listed every hold the journal ever carried,
// so a clean `revert --all` still printed its reverted holds each with a
// live-looking ttl_remaining and AC-6's own proof step ("`mischief status`
// empty") could never pass. The complete listing is StatusAll.
func (l *Lifecycle) Status() Status { return l.foldStatus(true) }

// StatusAll folds the COMPLETE hold listing — active and terminal — for the
// `status --all` surface and for consumers that need a terminal hold's own
// row: the daemon's health document counts reverted / revert_failed and
// flips ok=false on a live revert_failed from the terminal rows.
func (l *Lifecycle) StatusAll() Status { return l.foldStatus(false) }

// foldStatus folds the in-memory journal state into a Status. activeOnly
// keeps Holds to the active subset via HoldStatus.Active — ONE predicate
// shared by the default surface and `--all`, so the two listing modes can
// never disagree about which statuses are active.
func (l *Lifecycle) foldStatus(activeOnly bool) Status {
	s := l.store
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{BootID: s.BootID}
	holds := make([]HoldStatusEntry, 0)
	revertFailed := make([]HoldStatusEntry, 0)
	ids := make([]string, 0, len(s.holds))
	for id := range s.holds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		st := s.holds[id]
		entry := HoldStatusEntry{
			HoldID:  id,
			FaultID: st.Arm.FaultID,
			Target:  st.Arm.Target,
			Status:  st.Status,
			Expiry:  st.Expiry,
			StuckAt: st.StuckAt,
			LandPID: st.LandPID,
			OwnPID:  st.OwnPID,
		}
		if st.Status == StatusRevertFailed {
			// MISCHIEF-DF-54-R2: a terminal revert_failed hold records
			// the FACT that its fault may still be live — the condition
			// the daemon grades ok:false from the same rows. It is
			// collected on EVERY fold (active-only or complete) so the
			// default status surface and the complete one carry the same
			// liveness signal and can never disagree about it.
			revertFailed = append(revertFailed, entry)
		}
		if activeOnly && !st.Status.Active() {
			continue
		}
		holds = append(holds, entry)
	}
	out.Holds = holds
	out.RevertFailedHolds = revertFailed
	out.History = append([]Proof(nil), s.history...)
	return out
}

// StatusSnapshot folds the CURRENT in-memory state without touching the
// journal — the degraded read for a caller whose re-fold just failed
// (MISCHIEF-DF-30's health leg): it must not re-derive from a journal that
// just refused to fold, and it must not return a zero Status when the
// in-memory fold is populated. nil maps (a Store that never folded) read
// as empty, not as a panic or a fabricated state. It carries the same
// active-only Holds contract as Status.
func (l *Lifecycle) StatusSnapshot() Status { return l.foldStatus(true) }

// StatusSnapshotAll is the degraded read's COMPLETE listing (terminal holds
// included): the daemon's stale render must count reverted / revert_failed
// exactly as its live render does while the journal is unreadable — a
// degraded leg that dropped the terminal rows would report a live
// revert_failed fault as ok:true, which is the one reading health exists to
// prevent.
func (l *Lifecycle) StatusSnapshotAll() Status { return l.foldStatus(false) }

// OwnerMain is the DETACHED owner's entry point (AC-5): it blocks until the
// hold's expiry, then reverts with the owner role. It is re-exec'd by
// Detach (setsid shape) or run under systemd-run's RuntimeMaxSec scope; the
// CLI's death does not touch it. The owner works from the journal bytes
// alone: it opens the SAME dir, folds the same log, executes the same
// declarative inverse and check.
func OwnerMain(dir, holdID string, expiry time.Time) error {
	st, err := Open(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	lf := New(st)
	wait := time.Until(expiry)
	if wait > 0 {
		time.Sleep(wait)
	}
	_, err = lf.Revert(context.Background(), holdID, "ttl_expiry", RoleOwner)
	return err
}
