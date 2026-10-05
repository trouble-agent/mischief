package selftest

import (
	"testing"
)

// TestLiveSelftestAllGreenOrSkip is the M1 exit criterion as a go test:
// run the FULL covered corpus on the real backends (fresh scratch target
// per primitive) and require every result to be pass-or-skip. On this
// host the five rootless/probe-gated primitives must PASS (P-001, S-001,
// N-012, F-009, R-001); the recorded skips must name their missing piece.
// A red here means a primitive's land+revert loop stopped measuring on
// this host — the AC-19 set must not be touched up by hand.
func TestLiveSelftestAllGreenOrSkip(t *testing.T) {
	res := RunAllPublic()
	if len(res) != len(CoveredIDs()) {
		t.Fatalf("ran %d primitives, want %d", len(res), len(CoveredIDs()))
	}
	for _, r := range res {
		switch r.State {
		case "pass":
			if r.LandProof == "" || r.RevertProof == "" {
				t.Errorf("%s PASSED without both proofs (land=%q revert=%q)", r.ID, r.LandProof, r.RevertProof)
			}
		case "skip":
			if r.Reason == "" {
				t.Errorf("%s SKIPped without naming the missing piece", r.ID)
			}
		default:
			t.Errorf("%s %s: %s (land=%q revert=%q)", r.ID, r.State, r.Reason, r.LandProof, r.RevertProof)
		}
	}
	// the green set: exactly the covered, non-skip ids, all passing
	for _, id := range GreenIDs() {
		found := false
		for _, r := range res {
			if r.ID == id {
				found = true
				if r.State != "pass" {
					t.Errorf("%s is in the green set but graded %q (%s)", id, r.State, r.Reason)
				}
				break
			}
		}
		if !found {
			t.Errorf("%s is in the green set but never ran", id)
		}
	}
}
