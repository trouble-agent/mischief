package reverter

import (
	"context"
	"encoding/json"
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
	if len(s.Holds) != 1 || s.Holds[0].Status != StatusReverted {
		t.Fatalf("hold status after reopen: %+v", s.Holds)
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
	// the bad hold's inverse points at a nonexistent backup: its revert
	// measurably fails (the live file keeps its faulted bytes).
	bad.Inverse.Params["backup"] = badBackup + "-MISSING"
	idGood, err := lf.Arm(good)
	if err != nil {
		t.Fatalf("arm good: %v", err)
	}
	idBad, err := lf.Arm(bad)
	if err != nil {
		t.Fatalf("arm bad: %v", err)
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
