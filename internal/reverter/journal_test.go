package reverter

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// testContext is the suite's standard revert context (named so test bodies
// stay honest about the cancellation surface they exercise).
func testContext() context.Context { return context.Background() }

// jsonUnmarshalStrict is the test helper's indirection over json.Unmarshal.
func jsonUnmarshalStrict(b []byte, v any) error { return json.Unmarshal(b, v) }

// TestStoreFsyncedAppendSurvivesReopen: state lives ONLY in the journal —
// a fold from disk after a hard close reproduces the same holds, proofs and
// stuck marks (no in-memory-only state may exist).
func TestStoreFsyncedAppendSurvivesReopen(t *testing.T) {
	dir := localTempDir(t)
	st1, err := Open(dir)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	lf1 := New(st1)
	hold, _, _ := scratchHold(t, "P-DURABLE")
	id, err := lf1.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	if err := lf1.Land(id); err != nil {
		t.Fatalf("land: %v", err)
	}
	pr, err := lf1.Revert(testContext(), id, "direct", RoleAPI)
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}

	st2, err := Open(dir)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer st2.Close()
	s := New(st2).Status()
	if len(s.History) != 1 {
		t.Fatalf("history after reopen: %d proofs, want 1", len(s.History))
	}
	got := s.History[0]
	if got.HoldID != pr.HoldID || got.Outcome != pr.Outcome || got.FaultID != pr.FaultID {
		t.Fatalf("proof drift across reopen: %+v vs %+v", got, pr)
	}
	if got.Duration != pr.Duration {
		t.Fatalf("duration drift across reopen: %v vs %v", got.Duration, pr.Duration)
	}
	// The default status surface is the ACTIVE holds: the reverted hold is
	// terminal, so it is a FACT — carried in History (asserted above) and
	// in the complete listing (StatusAll), never as a live hold
	// (MISCHIEF-DF-54).
	if len(s.Holds) != 0 {
		t.Fatalf("terminal hold leaked into the active status surface: %+v", s.Holds)
	}
	complete := New(st2).StatusAll()
	if len(complete.Holds) != 1 || complete.Holds[0].Status != StatusReverted {
		t.Fatalf("hold status after reopen (complete listing): %+v", complete.Holds)
	}
	// a terminal hold never reverts again (the report, not a silent pass)
	if _, err := New(st2).Revert(testContext(), id, "direct", RoleAPI); err == nil {
		t.Fatal("second revert of a terminal hold returned nil")
	}
}

// TestRevertAllFailureProofsContinue: a hold whose measured revert FAILS
// does not stop the kill switch — every fault still gets its proof, the
// failed one grading revert_failed (the sweep is only as good as its worst
// proof, and it says so).
func TestRevertAllFailureProofsContinue(t *testing.T) {
	_, lf := newTestStore(t)
	good, _, _ := scratchHold(t, "P-SWEEP-GOOD")
	bad, badFile, badBackup := scratchHold(t, "P-SWEEP-BAD")
	idGood, err := lf.Arm(good)
	if err != nil {
		t.Fatalf("arm good: %v", err)
	}
	idBad, err := lf.Arm(bad)
	if err != nil {
		t.Fatalf("arm bad: %v", err)
	}
	// The bad hold's backup EXISTED at arm (DF-23's probe requires it) and
	// is sabotaged afterwards: its revert measurably fails (the live file
	// keeps its faulted bytes).
	if err := os.Remove(badBackup); err != nil {
		t.Fatalf("sabotage: %v", err)
	}
	proofs, _, err := lf.RevertAll(testContext(), "kill_switch")
	if err != nil {
		t.Fatalf("revert all: %v", err)
	}
	if len(proofs) != 2 {
		t.Fatalf("%d proofs, want 2", len(proofs))
	}
	byID := map[string]Proof{}
	for _, pr := range proofs {
		byID[pr.HoldID] = pr
	}
	if byID[idGood].Outcome != OutcomeReverted {
		t.Fatalf("good hold: %s (%s)", byID[idGood].Outcome, byID[idGood].Detail)
	}
	if byID[idBad].Outcome != OutcomeRevertFailed {
		t.Fatalf("bad hold: %s, want revert_failed", byID[idBad].Outcome)
	}
	// and the fault IS still there (the failed revert told the truth)
	b, err := osReadFile(badFile)
	if err != nil || string(b) != "FAULTED-CONTENT\n" {
		t.Fatalf("revert_failed hold's fault vanished: %q %v", b, err)
	}
}

// TestStatusSurface: the status surface reports boot id, per-hold state and
// TTL expiry — the shape the status verb and the future /health.json read.
func TestStatusSurface(t *testing.T) {
	_, lf := newTestStore(t)
	hold, _, _ := scratchHold(t, "P-STATUS")
	id, err := lf.Arm(hold)
	if err != nil {
		t.Fatalf("arm: %v", err)
	}
	s := lf.Status()
	if s.BootID == "" || len(s.BootID) != 16 {
		t.Fatalf("boot id %q is not hex16", s.BootID)
	}
	if len(s.Holds) != 1 {
		t.Fatalf("%d holds, want 1", len(s.Holds))
	}
	h := s.Holds[0]
	if h.HoldID != id || h.FaultID != hold.FaultID || h.Status != StatusArmed {
		t.Fatalf("status row drift: %+v", h)
	}
	if h.Expiry.IsZero() {
		t.Fatal("armed hold has no TTL expiry in status")
	}
	if time.Until(h.Expiry) <= 0 {
		t.Fatalf("expiry %v is not in the future", h.Expiry)
	}
}

// TestArmExpiryNeverBeforeAdvertisedTTL (MISCHIEF-DF-59): the fold derives the
// hold's expiry from the arm record's whole-second timestamp plus the TTL, so
// an arm at T+0.9s used to expire at T+30s with TTL 30s — a hold 0.9s SHORT of
// what the operator declared. The fold now places expiry one second beyond
// that arm second (the safe ceil on the second grid it can actually express),
// so the hold NEVER expires before the advertised TTL has fully elapsed,
// no matter where inside a second the arm landed.
//
// Boundary arms (DF-59: TTL 1s and TTL 30s): the hold must still be live
// (never reverted, expiry not passed) at now + TTL measured from the REAL arm
// instant, and its expiry must not overshoot the worst-case grid by more than
// the documented 1s.
func TestArmExpiryNeverBeforeAdvertisedTTL(t *testing.T) {
	for _, ttl := range []time.Duration{time.Second, 30 * time.Second} {
		t.Run(ttl.String(), func(t *testing.T) {
			st, lf := newTestStore(t)
			// Pin the arm instant at a sub-second offset (0.9s into the
			// second): the worst case of the old truncation — floor(TS)
			// loses exactly the 0.9s the operator counted on.
			armAt := time.Now().Truncate(time.Second).Add(900 * time.Millisecond)
			st.Now = func() time.Time { return armAt }
			hold, _, _ := scratchHold(t, "P-DF59-BOUNDARY")
			hold.TTL = ttl
			id, err := lf.Arm(hold)
			if err != nil {
				t.Fatalf("arm: %v", err)
			}

			// The advertised containment point: one full TTL after the
			// arm instant, sub-second part included. At that instant the
			// hold MUST still be live — an expiry earlier than this is
			// the truncation DF-59 forbids.
			advertised := armAt.Add(ttl)
			if !st.Now().Before(advertised.Add(time.Millisecond)) {
				t.Fatalf("test clock drifted past the advertised boundary: now=%v advertised=%v", st.Now(), advertised)
			}
			s := lf.Status()
			if len(s.Holds) != 1 || s.Holds[0].Status != StatusArmed {
				t.Fatalf("hold already terminal at its advertised TTL: %+v", s.Holds)
			}
			if s.Holds[0].Expiry.Before(advertised.Add(time.Millisecond)) {
				t.Fatalf("expiry %v is BEFORE the advertised containment point %v — hold expires short of its TTL", s.Holds[0].Expiry, advertised)
			}

			// Overshoot bound: expiry sits on the folded second grid, at
			// most one second past Unix(floor(arm)) + TTL (never two).
			grid := armAt.Truncate(time.Second).Add(ttl).Add(2 * time.Second)
			if s.Holds[0].Expiry.After(grid) {
				t.Fatalf("expiry %v overshoots the documented +1s grid bound %v", s.Holds[0].Expiry, grid)
			}

			// The re-fold (a fresh Open, the state every other process
			// sees) must derive the SAME expiry — the boundary lives in
			// the fold, not in one process's memory.
			if err := st.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			st2, err := Open(st.Dir)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer st2.Close()
			got := st2.holds[id].Expiry
			if !got.Equal(s.Holds[0].Expiry) {
				t.Fatalf("re-folded expiry %v != live expiry %v", got, s.Holds[0].Expiry)
			}
		})
	}
}
