package reverter

import (
	"context"
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

// Arm is the write-ahead half of the contract: the hold — fault, inverse,
// measured check, TTL — is appended to the journal and fsynced BEFORE the
// caller may land the fault. The returned hold id is content-derived
// (hex(sha256)[:16]); arming the same declaration twice yields the same id
// and a second arm line (the fold collapses duplicate states, not evidence).
func (l *Lifecycle) Arm(h Hold) (string, error) {
	if err := h.Validate(); err != nil {
		return "", fmt.Errorf("reverter: arm: %w", err)
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
// swallowed).
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
	terminal := st.Status == StatusReverted || st.Status == StatusRevertFailed
	arm := st.Arm
	expiry := st.Expiry
	s.mu.Unlock()

	// TTL ownership: an early manual revert before expiry is allowed
	// (operator kill switch per-fault), but the owner's expiry is the
	// contract for the detached path — passed through in the proof fields.
	_ = expiry

	if terminal {
		return Proof{}, fmt.Errorf("%w: %s (already terminal: %s)", ErrNoHold, holdID, st.Status)
	}

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
// CLI's fate that reverts the hold at expiry (AC-5). The proven shape
// (`systemd-run --user --scope -p RuntimeMaxSec=<ttl+margin>`) is used when
// the user manager is reachable; otherwise a real setsid child re-execs
// OwnerMain. The owner survives kill -9 of the CLI; it reverts from the
// journal bytes alone.
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
type Status struct {
	// BootID is this process generation's id.
	BootID string
	// Active holds: armed or stuck, with TTL remaining for live ones.
	Holds []HoldStatusEntry
	// History is every measured proof ever appended to the journal
	// (insertion order).
	History []Proof
}

// HoldStatusEntry is one hold's Status row.
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

// Status folds the current state.
func (l *Lifecycle) Status() Status {
	s := l.store
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{BootID: s.BootID}
	ids := make([]string, 0, len(s.holds))
	for id := range s.holds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		st := s.holds[id]
		out.Holds = append(out.Holds, HoldStatusEntry{
			HoldID:  id,
			FaultID: st.Arm.FaultID,
			Target:  st.Arm.Target,
			Status:  st.Status,
			Expiry:  st.Expiry,
			StuckAt: st.StuckAt,
			LandPID: st.LandPID,
			OwnPID:  st.OwnPID,
		})
	}
	out.History = append([]Proof(nil), s.history...)
	return out
}

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
