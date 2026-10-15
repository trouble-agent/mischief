// Tests for the AC-8 load gate on the battery landing path (MISCHIEF-DF-28):
// a saturated host refuses the whole matrix with the rails' load_gate
// refusal BEFORE any cell executes, and a healthy reading admits the run.
// Readings are INJECTED via the package ReadLoad seam (ambient-filesystem
// discipline — the suite never reads the box's real /proc numbers); the
// gate itself is left REAL (rails.LoadGate grades the pinned reading, so
// the refusal text is the production text, not a stub's).
//
// ch:trace row=MISCHIEF-DF-28 test=internal/battery/load_gate_test.go evidence=internal/battery/runner.go witness=none:injected-readings-no-live-run
package battery

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/trouble-agent/mischief/internal/catalog"
	"github.com/trouble-agent/mischief/internal/journal"
	"github.com/trouble-agent/mischief/internal/rails"
	"github.com/trouble-agent/mischief/internal/selftest"
)

// pinLoad pins the package ReadLoad seam to the given reading + err and
// returns the restore func (the doctor seams_test shape).
func pinLoad(t *testing.T, r rails.LoadReading, err error) {
	t.Helper()
	old := ReadLoad
	ReadLoad = func() (rails.LoadReading, error) { return r, err }
	t.Cleanup(func() { ReadLoad = old })
}

// saturatedReading is the dogfood run-12 shape (loadavg 1m 30.75) — the
// exact reading the gap was filed from.
func saturatedReading() rails.LoadReading {
	return rails.LoadReading{Load1: 30.75, Load5: 21.4, PSIIoSomeAvg10: 3.0}
}

// gateArmedRunner wires the production gate onto the stub executor: the
// NewRunner posture (gate + measurement) with the per-cell outcomes
// stubbed, so a green run proves the gate ADMITS rather than the harness
// running live primitives.
func gateArmedRunner(t *testing.T, cat *catalog.Catalog) *Runner {
	t.Helper()
	rn := NewRunner(cat)
	rn.executeCell = func(c Cell) selftest.Result {
		return selftest.Result{ID: c.Primitive, State: "pass", LandProof: "landed: proof", RevertProof: "reverted: proof"}
	}
	return rn
}

// TestBatteryRunRefusesSaturatedHost: over the load ceiling ⇒ Run returns
// the rails' load_gate refusal (verdict aborted, measured numbers vs the
// gate named), NOT a RunResult — the run "did not happen", the same class
// as the empty-matrix refusal. No cell executed: the executor seam is
// armed to FAIL LOUDLY if anything runs.
func TestBatteryRunRefusesSaturatedHost(t *testing.T) {
	pinHealthyLoad(t) // armed first; the saturated pin replaces it below
	rn := gateArmedRunner(t, stubCat("P-001"))
	executed := false
	rn.executeCell = func(c Cell) selftest.Result {
		executed = true
		t.Error("cell executed on a saturated host — the gate must refuse before the first cell")
		return selftest.Result{ID: c.Primitive, State: "pass", LandProof: "x", RevertProof: "y"}
	}
	pinLoad(t, saturatedReading(), nil)

	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001"), cell("c2", "P-001")}})
	if err == nil {
		t.Fatalf("saturated host admitted the matrix: %+v", res)
	}
	if executed {
		t.Fatal("the executor seam ran despite the gate refusal")
	}
	if !rails.IsRefusal(err) {
		t.Fatalf("error %T is not a rails refusal: %v", err, err)
	}
	ref, ok := err.(*rails.Refusal)
	if !ok {
		t.Fatalf("error is a refusal but not *rails.Refusal: %T", err)
	}
	if ref.Reason != rails.ReasonLoad {
		t.Fatalf("Reason=%q, want %q", ref.Reason, rails.ReasonLoad)
	}
	if ref.Verdict != rails.VerdictAborted {
		t.Fatalf("Verdict=%q, want %q (AC-8: refused grades aborted)", ref.Verdict, rails.VerdictAborted)
	}
	for _, want := range []string{"30.75", "12.00", "load_gate", "refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal text %q must name %q (AC-8: measured numbers vs the gate)", err.Error(), want)
		}
	}
	if res != nil {
		t.Fatalf("a refused run returned a RunResult (%d cells) — the run did not happen", len(res.Cells))
	}
	if rails.MapExit(err) != 2 {
		t.Fatalf("MapExit(load refusal)=%d, want 2", rails.MapExit(err))
	}
}

// TestBatteryRunHealthyReadingProceeds: under every ceiling ⇒ the run
// executes every cell and grades normally (the gate must not over-refuse
// a healthy host).
func TestBatteryRunHealthyReadingProceeds(t *testing.T) {
	pinHealthyLoad(t)
	rn := gateArmedRunner(t, stubCat("P-001"))
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}})
	if err != nil {
		t.Fatalf("healthy host refused: %v", err)
	}
	if len(res.Cells) != 1 || res.Cells[0].Verdict != journal.VerdictRecovered {
		t.Fatalf("gated-healthy run graded %+v, want the pass cell recovered", res.Cells)
	}
}

// TestBatteryRunPsiArmRefuses: the PSI arm alone breaches ⇒ refused (both
// gate arms are live on the landing path, not just the loadavg one).
func TestBatteryRunPsiArmRefuses(t *testing.T) {
	pinHealthyLoad(t)
	rn := gateArmedRunner(t, stubCat("P-001"))
	pinLoad(t, rails.LoadReading{Load1: 1.0, Load5: 0.9, PSIIoSomeAvg10: 88.5}, nil)
	_, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}})
	if err == nil || !rails.IsRefusal(err) {
		t.Fatalf("PSI-saturated host admitted the matrix: %v", err)
	}
	if !strings.Contains(err.Error(), "psi") || !strings.Contains(err.Error(), "88.50") {
		t.Fatalf("refusal does not name the PSI measurement: %v", err)
	}
}

// TestBatteryRunUnreadableLoadFailsClosed: a measurement error refuses the
// run (a gate that cannot say what it measured does not land faults) with
// a non-refusal error naming the gate — not a silent pass.
func TestBatteryRunUnreadableLoadFailsClosed(t *testing.T) {
	pinHealthyLoad(t)
	rn := gateArmedRunner(t, stubCat("P-001"))
	pinLoad(t, rails.LoadReading{}, errors.New("/proc vanished"))
	_, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}})
	if err == nil {
		t.Fatal("unreadable load admitted the matrix — fail-closed violated")
	}
	if rails.IsRefusal(err) {
		t.Fatalf("an unreadable measurement is not a host-refusal: %v", err)
	}
	if !strings.Contains(err.Error(), "load gate") {
		t.Fatalf("error does not name the gate: %v", err)
	}
}

// TestBatteryRunGateAfterValidation: the gate is the rails' LAST rail — a
// matrix that fails validation refuses for ITS reason even on a saturated
// host (the unknown-primitive refusal must not be masked by load_gate).
func TestBatteryRunGateAfterValidation(t *testing.T) {
	pinHealthyLoad(t)
	rn := gateArmedRunner(t, stubCat("P-001"))
	pinLoad(t, saturatedReading(), nil)
	_, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "NOPE-99")}})
	if err == nil {
		t.Fatal("unknown primitive admitted")
	}
	var unknown *ErrUnknownPrimitive
	if !errors.As(err, &unknown) {
		t.Fatalf("saturated host masked the unknown-primitive refusal with: %v", err)
	}
}

// TestNewRunnerWiresTheGate: the production constructor arms both seam
// halves (measurement + gate) — a nil gate alongside the live measurement
// is the misconfiguration Run refuses loudly.
func TestNewRunnerWiresTheGate(t *testing.T) {
	pinHealthyLoad(t)
	rn := NewRunner(nil)
	if rn.readLoad == nil || rn.loadGate == nil {
		t.Fatalf("NewRunner wired readLoad=%t loadGate=%t, want both armed (AC-8 enforced by construction)", rn.readLoad == nil, rn.loadGate == nil)
	}
	if r, err := rn.readLoad(); err != nil {
		t.Fatalf("wired readLoad errored: %v", err)
	} else if r.PSIIoSomeAvg10 < -1 {
		t.Fatalf("wired readLoad produced an impossible reading: %+v", r)
	}
	// the wired gate grades a saturated reading to a refusal (real gate)
	if err := rn.loadGate.Check(saturatedReading()); err == nil {
		t.Fatal("the wired gate passed the dogfood run-12 reading — thresholds not armed")
	}
}

// TestRunnerRunMisconfiguredGatePairing: readLoad without loadGate is the
// wiring bug — refused loudly, never silently ungated.
func TestRunnerRunMisconfiguredGatePairing(t *testing.T) {
	rn := &Runner{
		Cat: stubCat("P-001"),
		executeCell: func(c Cell) selftest.Result {
			return selftest.Result{ID: c.Primitive, State: "pass", LandProof: "l", RevertProof: "r"}
		},
		now:      func() time.Time { return time.Unix(1700000000, 0) },
		readLoad: func() (rails.LoadReading, error) { return healthyReading(), nil },
		// loadGate deliberately nil
	}
	if _, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}}); err == nil || !strings.Contains(err.Error(), "misconfigured") {
		t.Fatalf("half-wired gate admitted the run: %v", err)
	}
}

// TestStubRunnerWithoutGateStaysUngated: the pre-existing stub posture
// (nil readLoad) stays ungated — the grading-branch tests keep running
// deterministic regardless of the box's load.
func TestStubRunnerWithoutGateStaysUngated(t *testing.T) {
	rn := stubRunner(stubCat("P-001"), map[string]selftest.Result{"P-001": stubPass})
	// NOTE: ReadLoad pinned SATURATED here — proving the nil-seam runner
	// never consults it.
	pinLoad(t, saturatedReading(), nil)
	res, err := rn.Run(&Matrix{Cells: []Cell{cell("c1", "P-001")}})
	if err != nil {
		t.Fatalf("ungated stub runner refused via the pinned saturated reading: %v", err)
	}
	if len(res.Cells) != 1 {
		t.Fatalf("cells %d, want 1", len(res.Cells))
	}
}
