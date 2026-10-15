// Tests for the AC-8 load gate on the selftest landing path
// (MISCHIEF-DF-28): a saturated host records the gate refusal per
// primitive — the rails' load_gate shape, verdict aborted, the AC-11
// skip vocabulary — and never reaches the land+revert loop; a healthy
// reading admits the loop. Readings are INJECTED via the package ReadLoad
// seam (ambient-filesystem discipline — the suite never reads the box's
// real /proc numbers); the gate itself is left REAL (rails.LoadGate
// grades the pinned reading, so the refusal text is the production text).
//
// ch:trace row=MISCHIEF-DF-28 test=internal/selftest/load_gate_test.go evidence=internal/selftest/entry.go + internal/selftest/selftest.go witness=none:injected-readings-no-live-run
package selftest

import (
	"errors"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/rails"
)

// pinLoad pins the package ReadLoad seam to the given reading + err for
// the test's duration (the doctor seams_test shape).
func pinLoad(t *testing.T, r rails.LoadReading, err error) {
	t.Helper()
	old := ReadLoad
	ReadLoad = func() (rails.LoadReading, error) { return r, err }
	t.Cleanup(func() { ReadLoad = old })
}

// gateRunner is the production runner posture (gate + measurement wired)
// over ONE stub landable — the RunOne path is real, the primitive's
// mechanics are stubbed, so a green run proves the gate ADMITS rather
// than the harness running a live primitive.
func gateRunner(t *testing.T, l *stubLandable) runner {
	t.Helper()
	rn := newRunner()
	rn.corpus = func() []string { return []string{"Z-001"} }
	rn.build = func(id, dir string) landable { return l }
	return rn
}

// saturatedReading is the dogfood run-12 shape (loadavg 1m 30.75) — the
// exact reading the gap was filed from.
func saturatedReading() rails.LoadReading {
	return rails.LoadReading{Load1: 30.75, Load5: 21.4, PSIIoSomeAvg10: 3.0}
}

// TestSelftestRunOneRefusesSaturatedHost: over the load ceiling ⇒ the
// result is the AC-11 skip carrying the rails refusal (reason load_gate,
// the measured numbers vs the gate, verdict aborted named in the text),
// and the landable NEVER arms (prepare/land untouched). A skip — not a
// fail: the run "did not happen" is a recorded outcome, never a defect of
// the primitive, and the journal grades it aborted (the rails' verdict).
func TestSelftestRunOneRefusesSaturatedHost(t *testing.T) {
	pinHealthyLoad(t) // production posture first; the saturated pin replaces it
	l := &stubLandable{snap: []byte("pre")}
	l.landOut = ok("land measured")
	rn := gateRunner(t, l)
	pinLoad(t, saturatedReading(), nil)

	res := rn.RunOne("Z-001")
	if !res.IsSkip() {
		t.Fatalf("saturated host graded %q (%s), want the load_gate skip", res.State, res.Reason)
	}
	for _, want := range []string{"load_gate", "30.75", "12.00", "refused"} {
		if !strings.Contains(res.Reason, want) {
			t.Fatalf("skip reason %q must name %q (AC-8: measured numbers vs the gate)", res.Reason, want)
		}
	}
	if l.prepared || l.landed {
		t.Fatal("the landable armed on a saturated host — the gate must refuse before the loop")
	}
	// NOTE: no cleanup assertion here — the refusal fires BEFORE the
	// landable is built (nothing was armed, so there is nothing to
	// release; the cleanup-runs-on-every-path contract covers the loop's
	// own paths, which refusal never enters).
}

// TestSelftestRunOneHealthyReadingProceeds: under every ceiling ⇒ the
// loop runs and grades normally (the gate must not over-refuse a healthy
// host).
func TestSelftestRunOneHealthyReadingProceeds(t *testing.T) {
	pinHealthyLoad(t)
	l := &stubLandable{snap: []byte("same")}
	l.landOut = ok("land proof text")
	l.invOut = ok("revert proof text")
	rn := gateRunner(t, l)

	res := rn.RunOne("Z-001")
	if res.State != "pass" {
		t.Fatalf("healthy host graded %q (%s), want pass", res.State, res.Reason)
	}
	if res.LandProof != "land proof text" || res.RevertProof != "revert proof text" {
		t.Fatalf("gated-healthy pass lost its proofs: %+v", res)
	}
	if !l.landed {
		t.Fatal("the landable never armed under a healthy reading")
	}
}

// TestSelftestRunOnePsiArmRefuses: the PSI arm alone breaches ⇒ refused
// (both gate arms live on the landing path).
func TestSelftestRunOnePsiArmRefuses(t *testing.T) {
	pinHealthyLoad(t)
	l := &stubLandable{snap: []byte("pre")}
	rn := gateRunner(t, l)
	pinLoad(t, rails.LoadReading{Load1: 1.0, Load5: 0.9, PSIIoSomeAvg10: 88.5}, nil)

	res := rn.RunOne("Z-001")
	if !res.IsSkip() || !strings.Contains(res.Reason, "psi") || !strings.Contains(res.Reason, "88.50") {
		t.Fatalf("PSI-saturated host graded %q (%s), want the psi load_gate skip naming 88.50", res.State, res.Reason)
	}
}

// TestSelftestRunOneUnreadableLoadFailsClosed: a measurement error is a
// FAIL (the gate cannot say what it measured — a skip would read as "the
// rails refused", which is not what happened), naming the gate.
func TestSelftestRunOneUnreadableLoadFailsClosed(t *testing.T) {
	pinHealthyLoad(t)
	l := &stubLandable{snap: []byte("pre")}
	rn := gateRunner(t, l)
	pinLoad(t, rails.LoadReading{}, errors.New("/proc vanished"))

	res := rn.RunOne("Z-001")
	if res.State != "fail" || !strings.Contains(res.Reason, "load gate") {
		t.Fatalf("unreadable load graded %q (%s), want a fail naming the gate", res.State, res.Reason)
	}
	if l.prepared || l.landed {
		t.Fatal("the landable armed without a usable measurement (fail-closed violated)")
	}
}

// TestSelftestRunOneUnknownIdUngated: the gate is the rails' LAST rail —
// an id outside the corpus fails for ITS reason even on a saturated host
// (the unknown-primitive refusal must not be masked by load_gate).
func TestSelftestRunOneUnknownIdUngated(t *testing.T) {
	pinHealthyLoad(t)
	rn := gateRunner(t, &stubLandable{snap: []byte("x")})
	pinLoad(t, saturatedReading(), nil)

	res := rn.RunOne("NOPE-99")
	if res.State != "fail" || !strings.Contains(res.Reason, "unknown primitive") {
		t.Fatalf("saturated host masked the unknown-primitive refusal: %q (%s)", res.State, res.Reason)
	}
}

// TestSelftestRefusedSkipGradesAbortedInJournal: a refused-only run's
// journal grades aborted — the verdict a rails refusal carries (the
// skip⇒aborted mapping already in JournalRecords now has a real producer).
func TestSelftestRefusedSkipGradesAbortedInJournal(t *testing.T) {
	res := []Result{refused("P-001", gateReason(errLoadRefusal))}
	verdict, closed := "", ""
	for _, r := range JournalRecords("loadgatetest0000001", res) {
		switch r.Type {
		case "verdict":
			verdict = string(r.Verdict)
		case "run_closed":
			closed = string(r.Verdict)
		}
	}
	if verdict != "aborted" || closed != "aborted" {
		t.Fatalf("refused run graded verdict=%s run_closed=%s, want aborted/aborted", verdict, closed)
	}
}

// errLoadRefusal is a stand-in rails refusal text (the real shape is
// covered by rails' own suite; here the mapping is what is under test).
var errLoadRefusal = errors.New("rails: load_gate: host saturated, fault refused — measured loadavg 1m 30.75 >= gate 12.00")

// TestNewRunnerWiresTheGate: the production runner arms both seam halves
// (measurement + gate) — AC-8 enforced by construction, not by call-site
// discipline.
func TestNewRunnerWiresTheGate(t *testing.T) {
	rn := newRunner()
	if rn.readLoad == nil {
		t.Fatal("newRunner wired no measurement seam — the gate cannot run")
	}
	if rn.checkLoad == nil {
		t.Fatal("newRunner wired no gate seam — the gate cannot run")
	}
	// the wired gate grades the saturated dogfood reading to a refusal
	if err := rn.gateCheck(saturatedReading()); err == nil {
		t.Fatal("the wired gate passed the dogfood run-12 reading — thresholds not armed")
	}
	// and a healthy reading passes
	if err := rn.gateCheck(rails.LoadReading{Load1: 1.0, Load5: 0.8, PSIIoSomeAvg10: 2.0}); err != nil {
		t.Fatalf("the wired gate refused a healthy reading: %v", err)
	}
}

// TestGateThresholdsMatchDoctor: the landing paths gate at the SAME
// numbers doctor reports (load 12.0, PSI 50.0) — a host doctor grades
// saturated must be refused here, and vice versa (the two surfaces agree).
func TestGateThresholdsMatchDoctor(t *testing.T) {
	gate, err := newGate()
	if err != nil {
		t.Fatalf("newGate: %v", err)
	}
	// doctor's exact boundary: at-or-above refuses (its LoadThreshold
	// arm), below admits
	if err := gate.Check(rails.LoadReading{Load1: 12.0, Load5: 0.1, PSIIoSomeAvg10: -1}); err == nil {
		t.Fatal("loadavg at doctor's threshold 12.0 admitted — the surfaces disagree")
	}
	if err := gate.Check(rails.LoadReading{Load1: 11.99, Load5: 0.1, PSIIoSomeAvg10: 49.99}); err != nil {
		t.Fatalf("reading under both doctor thresholds refused: %v", err)
	}
}
