package backend_sim

// load_gate_test.go — the AC-8 load gate on the sim landing path
// (MISCHIEF-DF-65). Readings are INJECTED via the package ReadLoad seam
// (ambient-filesystem discipline — the suite never reads the box's real
// /proc numbers); the gate itself is left REAL (rails.LoadGate grades the
// pinned reading, so the refusal text is the production text).
//
// ch:trace row=MISCHIEF-DF-65 spec=docs/SPEC-PLAN.md#SPEC-03 test=internal/backend_sim/load_gate_test.go evidence=internal/backend_sim/load_gate.go witness=none:injected-readings-no-live-run

import (
	"errors"
	"strings"
	"testing"

	"github.com/trouble-agent/mischief/internal/rails"
)

// pinLoad pins the package ReadLoad seam to the given reading + err for
// the test's duration (the selftest/battery pinLoad shape).
func pinLoad(t *testing.T, r rails.LoadReading, err error) {
	t.Helper()
	old := ReadLoad
	ReadLoad = func() (rails.LoadReading, error) { return r, err }
	t.Cleanup(func() { ReadLoad = old })
}

// saturatedReading is the dogfood run-26 shape (loadavg 1m 21.44) — the
// exact reading the sim-leg half of the pair was filed from (battery
// refused at the same minute; sim landed).
func saturatedReading() rails.LoadReading {
	return rails.LoadReading{Load1: 21.44, Load5: 18.2, PSIIoSomeAvg10: 3.0}
}

// TestCheckLoadGateRefusesSaturatedHost: over the load ceiling ⇒ the
// rails refusal verbatim — reason load_gate, the measured numbers vs the
// gate, "refused" named (AC-8: the reason carries the measurement).
func TestCheckLoadGateRefusesSaturatedHost(t *testing.T) {
	pinLoad(t, saturatedReading(), nil)
	err := CheckLoadGate()
	if err == nil {
		t.Fatal("saturated host admitted — the sim landing path's gate is not armed")
	}
	for _, want := range []string{"load_gate", "21.44", "12.00", "refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q must name %q (AC-8: measured numbers vs the gate)", err.Error(), want)
		}
	}
	if !rails.IsRefusal(err) {
		t.Fatalf("refusal is not a rails refusal (the CLI's exit-2 mapping depends on it): %v", err)
	}
}

// TestCheckLoadGateAdmitsHealthyReading: under every ceiling ⇒ nil (the
// gate must not over-refuse a healthy host — the landing proceeds).
func TestCheckLoadGateAdmitsHealthyReading(t *testing.T) {
	pinLoad(t, rails.LoadReading{Load1: 1.0, Load5: 0.8, PSIIoSomeAvg10: 2.0}, nil)
	if err := CheckLoadGate(); err != nil {
		t.Fatalf("healthy reading refused: %v", err)
	}
}

// TestCheckLoadGatePsiArmRefuses: the PSI arm alone breaches ⇒ refused
// (both gate arms live on the sim landing path too).
func TestCheckLoadGatePsiArmRefuses(t *testing.T) {
	pinLoad(t, rails.LoadReading{Load1: 1.0, Load5: 0.9, PSIIoSomeAvg10: 88.5}, nil)
	err := CheckLoadGate()
	if err == nil {
		t.Fatal("PSI-saturated host admitted")
	}
	if !strings.Contains(err.Error(), "psi") || !strings.Contains(err.Error(), "88.50") {
		t.Fatalf("refusal %q must name the psi arm and its measurement", err.Error())
	}
}

// TestCheckLoadGateUnreadableMeasurementFailsClosed: a measurement error
// is a refusal (fail closed — a gate that cannot say what it measured
// does not run), naming the gate.
func TestCheckLoadGateUnreadableMeasurementFailsClosed(t *testing.T) {
	pinLoad(t, rails.LoadReading{}, errors.New("/proc vanished"))
	err := CheckLoadGate()
	if err == nil {
		t.Fatal("unreadable measurement admitted the landing (fail-closed violated)")
	}
	if !strings.Contains(err.Error(), "load gate") || !strings.Contains(err.Error(), "/proc vanished") {
		t.Fatalf("measurement-failure refusal must name the gate and the error: %v", err)
	}
}

// TestCheckLoadGateThresholdsMatchDoctor: the sim path gates at the SAME
// numbers doctor reports (load 12.0, PSI 50.0) — a host doctor grades
// saturated must be refused here and vice versa (the surfaces agree;
// 11.99/49.99 under, 12.0 at-the-boundary refuses).
func TestCheckLoadGateThresholdsMatchDoctor(t *testing.T) {
	pinLoad(t, rails.LoadReading{Load1: 11.99, Load5: 0.1, PSIIoSomeAvg10: 49.99}, nil)
	if err := CheckLoadGate(); err != nil {
		t.Fatalf("reading under both doctor thresholds refused: %v", err)
	}
	pinLoad(t, rails.LoadReading{Load1: 12.0, Load5: 0.1, PSIIoSomeAvg10: -1}, nil)
	if err := CheckLoadGate(); err == nil {
		t.Fatal("loadavg at doctor's threshold 12.0 admitted — the surfaces disagree")
	}
}
